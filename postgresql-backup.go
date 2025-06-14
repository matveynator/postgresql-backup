//go:build !windows
// +build !windows

// postgresql-backup — hot physical backup of a local PostgreSQL cluster
// pure Go, без shell-команд.

package main

import (
	"archive/tar"
	"bufio" // ← вернули: нужен parseFTPConf
	"compress/gzip"
	"database/sql"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jlaffaye/ftp"
	_ "github.com/lib/pq" // PostgreSQL driver
)

/******************** CONFIG & GLOBALS ********************/

var (
	backupPath string // root for all backups
	keepDays   int    // local retention (days)
	maxCopies  int    // keep only N newest daily archives (0 = unlimited)

	// PostgreSQL
	pgDSN string // connection string

	// FTP
	ftpConfFile          string
	ftpHost, ftpUser     string
	ftpPass              string
	ftpKeepFactor        int
	ftpEnabled           bool
	ftpKeepFactorFlagged bool
)

const (
	green  = "\033[32m"
	yellow = "\033[33m"
	red    = "\033[31m"
	cyan   = "\033[36m"
	reset  = "\033[0m"

	lockFile     = "/tmp/postgresql_backup.lock"
	backupSubdir = "postgresql-backup"
)

type ftpAccount struct{ Host, User, Pass string }

var ftpAccounts []ftpAccount

/******************** MAIN ********************/

func main() {
	// Flags
	listFlag := flag.Bool("list", false, "List existing backups and exit")
	helpFlag := flag.Bool("help", false, "Show help and exit")

	flag.StringVar(&backupPath, "backup-path", "/backup", "Root directory for backups")
	flag.IntVar(&keepDays, "days", 30, "Days to keep local daily backups")
	flag.IntVar(&maxCopies, "copies", 0, "Keep only <n> newest daily backups (0 = unlimited)")
	flag.IntVar(&maxCopies, "c", 0, "Alias for --copies")
	flag.StringVar(&pgDSN, "dsn",
		"host=/var/run/postgresql user=postgres sslmode=disable",
		"PostgreSQL DSN (connection string)")

	// FTP
	flag.StringVar(&ftpConfFile, "ftp-conf", "/etc/ftp-backup.conf", "Path to FTP credentials file")
	flag.StringVar(&ftpHost, "ftp-host", "", "Override FTP host")
	flag.StringVar(&ftpUser, "ftp-user", "", "Override FTP username")
	flag.StringVar(&ftpPass, "ftp-pass", "", "Override FTP password")
	flag.IntVar(&ftpKeepFactor, "ftp-keep-factor", 4, "Retention multiplier on FTP")

	flag.Parse()

	if *helpFlag {
		printHelp()
		return
	}
	if *listFlag {
		listBackups()
		return
	}

	// если пользователь задал --ftp-keep-factor вручную
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "ftp-keep-factor" {
			ftpKeepFactorFlagged = true
		}
	})
	// maxCopies=1 → по умолчанию храним на FTP в 4 раза дольше
	if !ftpKeepFactorFlagged && maxCopies == 1 {
		ftpKeepFactor = 4
	}

	initFTP()

	acquireLock()
	defer releaseLock()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() { <-sig; releaseLock(); os.Exit(1) }()

	runBackup()
}

/******************** HELP & LIST ********************/

func printHelp() {
	exe := filepath.Base(os.Args[0])
	fmt.Printf("%s📦 PostgreSQL Backup Utility%s\n\n", cyan, reset)
	fmt.Printf("Usage:\n  %s [flags]\n\n", exe)
	fmt.Println("Flags:")
	fmt.Println("  --dsn <conn>             PostgreSQL DSN (default: local socket)")
	fmt.Println("  --backup-path <dir>      Root directory for backups (/backup)")
	fmt.Println("  --days <n>               Days to keep local daily backups (30)")
	fmt.Println("  --copies, -c <n>         Keep only N newest daily archives (0 = unlimited)")
	fmt.Println("  --list                   List backups and exit")
	fmt.Println("  --ftp-conf <file>        FTP credentials file (/etc/ftp-backup.conf)")
	fmt.Println("  --ftp-host/user/pass     Override credentials from file")
	fmt.Println("  --ftp-keep-factor <n>    Days on FTP = days * n (default 4)")
}

func listBackups() {
	host, _ := os.Hostname()
	root := filepath.Join(backupPath, host, backupSubdir, "cluster", "daily")
	files, err := os.ReadDir(root)
	if err != nil {
		log.Fatalf("%sCannot open %s: %v%s", red, root, err, reset)
	}
	for _, f := range files {
		fmt.Println(f.Name())
	}
}

/******************** BACKUP LOOP ********************/

func runBackup() {
	now := time.Now()
	host, _ := os.Hostname()

	db, err := sql.Open("postgres", pgDSN)
	if err != nil {
		log.Fatalf("%sCannot connect to PostgreSQL: %v%s", red, err, reset)
	}
	defer db.Close()

	// 1) start backup
	var lsn string
	if err := db.QueryRow(`SELECT lsn FROM pg_backup_start(false)`).Scan(&lsn); err != nil {
		// fallback ≤14
		if err := db.QueryRow(`SELECT pg_start_backup('go-backup', true)`).Scan(&lsn); err != nil {
			log.Fatalf("%sCannot start backup: %v%s", red, err, reset)
		}
	}
	log.Printf("%s🚀 Backup started at LSN %s%s", cyan, lsn, reset)

	// 2) data_directory
	var dataDir string
	if err := db.QueryRow(`SHOW data_directory`).Scan(&dataDir); err != nil {
		log.Fatalf("%sCannot determine data_directory: %v%s", red, err, reset)
	}

	// 3) archive
	archivePath := backupCluster(dataDir, host, now)

	// 4) stop backup
	if _, err := db.Exec(`SELECT pg_backup_stop(false)`); err != nil {
		_, _ = db.Exec(`SELECT pg_stop_backup()`) // fallback
	}
	log.Printf("%s✅ Backup finished%s", green, reset)

	// 5) FTP
	if ftpEnabled && archivePath != "" {
		rel := strings.TrimPrefix(archivePath, backupPath)
		rel = strings.TrimPrefix(rel, string(os.PathSeparator))
		uploadToFTP(archivePath, rel)
	}
}

/******************** BACKUP HELPERS ********************/

func backupCluster(dataDir, host string, now time.Time) string {
	base := filepath.Join(backupPath, host, backupSubdir, "cluster")
	daily := filepath.Join(base, "daily")
	weekly := filepath.Join(base, "weekly")
	monthly := filepath.Join(base, "monthly")
	yearly := filepath.Join(base, "yearly")
	for _, d := range []string{daily, weekly, monthly, yearly} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			log.Printf("%smkdir %s: %v%s", red, d, err, reset)
			return ""
		}
	}

	ts := now.Format("2006-01-02_15-04-05")
	archive := filepath.Join(daily, fmt.Sprintf("%s_cluster.tar.gz", ts))

	log.Printf("%s📦 Archiving %s …%s", cyan, archive, reset)
	if err := createTarGzFromDir(archive, dataDir); err != nil {
		log.Printf("%sArchive error: %v%s", red, err, reset)
		return ""
	}
	printFileSize(archive)

	if now.Weekday() == time.Sunday {
		copyFile(archive, filepath.Join(weekly, filepath.Base(archive)))
	}
	if now.Day() == 1 {
		copyFile(archive, filepath.Join(monthly, filepath.Base(archive)))
	}
	if now.YearDay() == 1 {
		copyFile(archive, filepath.Join(yearly, filepath.Base(archive)))
	}

	if maxCopies > 0 {
		rotateCopies(daily, maxCopies)
	} else {
		cleanupOldFiles(daily, keepDays)
	}
	return archive
}

/* recursive tar.gz of a directory */
func createTarGzFromDir(dst, dir string) error {
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	gw := gzip.NewWriter(out)
	defer gw.Close()
	tw := tar.NewWriter(gw)
	defer tw.Close()

	return filepath.Walk(dir, func(path string, info fs.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = rel
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		if _, err := io.Copy(tw, f); err != nil {
			f.Close()
			return err
		}
		f.Close()
		return nil
	})
}

/******************** FTP ****************************/

func initFTP() {
	// 1) from conf file
	if _, err := os.Stat(ftpConfFile); err == nil {
		_ = parseFTPConf(ftpConfFile)
	}
	// 2) override
	if ftpHost != "" {
		ftpAccounts = []ftpAccount{{Host: ftpHost, User: ftpUser, Pass: ftpPass}}
	}
	ftpEnabled = len(ftpAccounts) > 0
	if !ftpEnabled {
		return
	}
	for _, acc := range ftpAccounts {
		log.Printf("%s🌐 FTP target → %s (user %s)%s", cyan, acc.Host, acc.User, reset)
	}
}

func parseFTPConf(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	var cur ftpAccount
	commit := func() {
		if cur.Host != "" && cur.User != "" && cur.Pass != "" {
			ftpAccounts = append(ftpAccounts, cur)
		}
		cur = ftpAccount{}
	}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}
		kv := strings.SplitN(line, "=", 2)
		key := strings.TrimSpace(kv[0])
		val := strings.TrimSpace(kv[1])
		switch key {
		case "FTP_HOST":
			if cur.Host != "" {
				commit()
			}
			cur.Host = val
		case "FTP_USER":
			cur.User = val
		case "FTP_PASS":
			cur.Pass = val
		}
	}
	commit()
	return scanner.Err()
}

func uploadToFTP(localPath, remoteRel string) {
	for _, acc := range ftpAccounts {
		uploadToSingleFTP(acc, localPath, remoteRel)
	}
}

func uploadToSingleFTP(acc ftpAccount, localPath, remoteRel string) {
	c, err := ftp.Dial(acc.Host + ":21")
	if err != nil {
		log.Printf("%sFTP dial %s: %v%s", red, acc.Host, err, reset)
		return
	}
	defer c.Quit()
	if err := c.Login(acc.User, acc.Pass); err != nil {
		log.Printf("%sFTP login %s: %v%s", red, acc.Host, err, reset)
		return
	}

	// create dirs
	parts := strings.Split(filepath.Dir(remoteRel), string(os.PathSeparator))
	cwd := "/"
	for _, p := range parts {
		if p == "" {
			continue
		}
		cwd = filepath.Join(cwd, p)
		_ = c.MakeDir(cwd)
	}

	f, err := os.Open(localPath)
	if err != nil {
		log.Printf("%sFTP open local: %v%s", red, err, reset)
		return
	}
	defer f.Close()

	remotePath := filepath.ToSlash(remoteRel)
	log.Printf("%s⇪ Uploading to %s: %s%s", cyan, acc.Host, remotePath, reset)
	if err := c.Stor(remotePath, f); err != nil {
		log.Printf("%sFTP upload %s: %v%s", red, acc.Host, err, reset)
		return
	}

	// rotation for daily
	if strings.Contains(remotePath, "/daily/") {
		remoteDailyDir := filepath.ToSlash(filepath.Dir(remotePath))
		if maxCopies > 0 {
			rotateCopiesFTP(c, remoteDailyDir, maxCopies*ftpKeepFactor)
		} else {
			cleanupOldFilesFTP(c, remoteDailyDir, keepDays*ftpKeepFactor)
		}
	}
}

func rotateCopiesFTP(c *ftp.ServerConn, dir string, copies int) {
	entries, err := c.List(dir)
	if err != nil {
		return
	}
	var files []*ftp.Entry
	for _, e := range entries {
		if e.Type == ftp.EntryTypeFile && strings.HasSuffix(e.Name, ".tar.gz") {
			files = append(files, e)
		}
	}
	if len(files) <= copies {
		return
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].Time.After(files[j].Time)
	})
	for _, e := range files[copies:] {
		remoteFile := filepath.ToSlash(filepath.Join(dir, e.Name))
		log.Printf("🧹 (FTP) Deleting extra archive %s", remoteFile)
		_ = c.Delete(remoteFile)
	}
}

func cleanupOldFilesFTP(c *ftp.ServerConn, dir string, days int) {
	entries, err := c.List(dir)
	if err != nil {
		return
	}
	cutoff := time.Now().AddDate(0, 0, -days)
	for _, e := range entries {
		if e.Type != ftp.EntryTypeFile {
			continue
		}
		if e.Time.Before(cutoff) {
			remoteFile := filepath.ToSlash(filepath.Join(dir, e.Name))
			log.Printf("🧹 (FTP) Deleting old archive %s", remoteFile)
			_ = c.Delete(remoteFile)
		}
	}
}

/******************** FILE OPS ********************/

func createTarGz(dst string, files []string) error {
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	gw := gzip.NewWriter(out)
	defer gw.Close()
	tw := tar.NewWriter(gw)
	defer tw.Close()

	for _, file := range files {
		info, err := os.Stat(file)
		if err != nil {
			return err
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = filepath.Base(file)
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		f, err := os.Open(file)
		if err != nil {
			return err
		}
		if _, err := io.Copy(tw, f); err != nil {
			f.Close()
			return err
		}
		f.Close()
	}
	return nil
}

func copyFile(src, dst string) {
	in, err := os.Open(src)
	if err != nil {
		log.Printf("open %s: %v", src, err)
		return
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		log.Printf("create %s: %v", dst, err)
		return
	}
	defer out.Close()
	_, _ = io.Copy(out, in)
	_ = os.Chmod(dst, 0644)
}

func printFileSize(path string) {
	if info, err := os.Stat(path); err == nil {
		size := float64(info.Size()) / (1024 * 1024)
		log.Printf("%s💾 Archive size: %.2f MB%s", green, size, reset)
	}
}

/******************** ROTATION / CLEANUP ********************/

func rotateCopies(dir string, copies int) {
	files, _ := filepath.Glob(filepath.Join(dir, "*.tar.gz"))
	if len(files) <= copies {
		return
	}
	sort.Slice(files, func(i, j int) bool {
		fi, _ := os.Stat(files[i])
		fj, _ := os.Stat(files[j])
		return fi.ModTime().After(fj.ModTime())
	})
	for _, f := range files[copies:] {
		log.Printf("🧹 Deleting extra archive %s", filepath.Base(f))
		_ = os.Remove(f)
	}
}

func cleanupOldFiles(dir string, days int) {
	files, _ := filepath.Glob(filepath.Join(dir, "*.tar.gz"))
	cutoff := time.Now().AddDate(0, 0, -days)
	for _, f := range files {
		if info, err := os.Stat(f); err == nil && info.ModTime().Before(cutoff) {
			log.Printf("🧹 Deleting old archive %s", filepath.Base(f))
			_ = os.Remove(f)
		}
	}
}

/******************** LOCK ********************/

func acquireLock() {
	try := func() error {
		f, err := os.OpenFile(lockFile, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
		if err != nil {
			return err
		}
		defer f.Close()
		_, _ = f.WriteString(strconv.Itoa(os.Getpid()))
		return nil
	}
	if err := try(); err == nil {
		return
	}
	// stale?
	data, _ := os.ReadFile(lockFile)
	if pid, _ := strconv.Atoi(strings.TrimSpace(string(data))); pid > 0 {
		if proc, _ := os.FindProcess(pid); proc != nil &&
			proc.Signal(syscall.Signal(0)) == nil {
			log.Fatalf("%sBackup already running (PID %d)%s", red, pid, reset)
		}
	}
	_ = os.Remove(lockFile)
	if err := try(); err != nil {
		log.Fatalf("%sCannot create lock file: %v%s", red, err, reset)
	}
}

func releaseLock() { _ = os.Remove(lockFile) }
