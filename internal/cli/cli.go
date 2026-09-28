// Command laowangbot 是一个小巧的 Telegram userbot：单个静态 Go 二进制，
// 命令用 Go 写，状态存在 JSON 文件里。它读的 config.json 和 MiBox 写的
// 是同一个文件，所以已有的账号可以直接沿用。
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
	_ "time/tzdata"

	"github.com/OrionG-hub/laowangbot/internal/app"
	"github.com/OrionG-hub/laowangbot/internal/backup"
	"github.com/OrionG-hub/laowangbot/internal/bot"
	"github.com/OrionG-hub/laowangbot/internal/command"
	"github.com/OrionG-hub/laowangbot/internal/commands"
	"github.com/OrionG-hub/laowangbot/internal/config"
	"github.com/OrionG-hub/laowangbot/internal/extensions"
	"github.com/OrionG-hub/laowangbot/internal/login"
	"github.com/OrionG-hub/laowangbot/internal/logtail"
	"github.com/OrionG-hub/laowangbot/internal/migration"
	"github.com/OrionG-hub/laowangbot/internal/platform"
	"github.com/OrionG-hub/laowangbot/internal/plugin"
	"github.com/OrionG-hub/laowangbot/internal/sysinfo"
	"github.com/OrionG-hub/laowangbot/internal/verify"
)

// version 在构建时注入（scripts/build.sh）。
var version = ""

func Main(buildVersion string) {
	version = buildVersion
	var (
		pluginAction = flag.String("plugin", "", "list|install-local|replace-local|install|update")
		pluginSource = flag.String("plugin-source", "", "local directory or catalog plugin name")
		supervise    = flag.Bool("supervise", false, "serve under the portable restart supervisor")
		migrateFrom  = flag.String("migrate", "", "import an old deployment into a new empty --root")
		sourceKind   = flag.String("from", "auto", "migration source: auto, mibot-lite, mibox, telebox")
		root         = flag.String("root", ".", "deployment directory holding config.json")
		serve        = flag.Bool("serve", false, "connect and serve commands")
		check        = flag.Bool("check", false, "validate the account and session without connecting")
		signIn       = flag.Bool("login", false, "sign a Telegram account in and write config.json under --root")
		force        = flag.Bool("force", false, "with --login or --restore, replace an existing account")
		restoreFrom  = flag.String("restore", "", "unpack a backup made by .bf or --backup into --root; the service on that directory must be stopped")
		backupTo     = flag.String("backup", "", "write the same backup .bf sends, to a file, without connecting")
		apiID        = flag.Int("api-id", 0, "with --login, the api_id from my.telegram.org")
		apiHash      = flag.String("api-hash", "", "with --login, the api_hash from my.telegram.org")
		importMibox  = flag.String("import-mibox", "", "copy the plugin data files of a MiBox deployment directory into --root/data")
		check2       = flag.Bool("verify", false, "connect and run every read-only command against the live account, in Saved Messages")
		verbose      = flag.Bool("verbose", false, "log at debug level, including the protocol trace")
		showVersion  = flag.Bool("version", false, "print the version and exit")
	)
	flag.Parse()

	if *showVersion {
		fmt.Println(displayVersion())
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if *supervise {
		binary, err := os.Executable()
		if err == nil {
			err = platform.Supervise(ctx, binary, []string{"--serve", "--root", *root}, os.Stdin, os.Stdout, os.Stderr)
		}
		if err != nil && !errors.Is(err, context.Canceled) {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	if *pluginAction != "" {
		if err := os.MkdirAll(*root, 0700); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		lock, err := platform.LockRoot(*root)
		if err != nil {
			fmt.Fprintln(os.Stderr, "stop the bot or use Telegram tpm:", err)
			os.Exit(1)
		}
		defer lock.Close()
		m := plugin.Manager{Root: *root}
		switch *pluginAction {
		case "list":
			items, e := m.List()
			err = e
			for _, v := range items {
				fmt.Println(v.Name, v.Version)
			}
		case "install-local":
			err = m.InstallLocal(*pluginSource)
		case "replace-local":
			err = m.ReplaceLocal(*pluginSource)
		case "install":
			err = m.InstallRemote(ctx, *pluginSource)
		case "update":
			err = m.UpdateRemote(ctx, *pluginSource)
		default:
			err = fmt.Errorf("unknown plugin action %q", *pluginAction)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if *migrateFrom != "" {
		names := map[string]bool{}
		probe := &app.App{Root: *migrateFrom, Registry: command.New([]string{"."}, slog.Default())}
		commands.RegisterAll(probe)
		for _, c := range probe.Registry.Commands() {
			names[c.Name] = true
		}
		report, err := migration.Run(migration.Options{Source: *migrateFrom, Destination: *root, Kind: *sourceKind, Builtins: names, Convert: commands.ImportMiBox})
		if err != nil {
			fmt.Fprintln(os.Stderr, "migration:", err)
			os.Exit(1)
		}
		fmt.Printf("Migrated %s: %d files; report: %s\n", report.Kind, len(report.Files), filepath.Join(*root, "migration-report.json"))
		return
	}
	if *importMibox != "" {
		if err := commands.ImportMiBox(*importMibox, filepath.Join(*root, "data"), os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, "laowangbot --import-mibox:", err)
			os.Exit(1)
		}
		if !*serve && !*check && !*check2 {
			return
		}
	}
	if *backupTo != "" {
		archive, names, err := backup.Create(*root, displayVersion(), time.Now())
		if err == nil {
			err = os.WriteFile(*backupTo, archive, 0o600)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "laowangbot --backup:", err)
			os.Exit(1)
		}
		fmt.Printf("Backed up %d files to %s. It holds the login session: keep it private.\n", len(names), *backupTo)
		return
	}
	if *restoreFrom != "" {
		if err := restore(*restoreFrom, *root, *force); err != nil {
			fmt.Fprintln(os.Stderr, "laowangbot --restore:", err)
			os.Exit(1)
		}
		return
	}
	if *signIn {
		if err := login.Run(ctx, login.Options{Root: *root, APIID: *apiID, APIHash: *apiHash, Force: *force}); err != nil {
			fmt.Fprintln(os.Stderr, "laowangbot --login:", err)
			os.Exit(1)
		}
		return
	}
	if !*serve && !*check && !*check2 {
		fmt.Fprintln(os.Stderr, "usage: laowangbot --serve | --check | --verify | --login [--force] | --backup FILE | --restore FILE [--force] | --import-mibox DIR [--root DIR]")
		os.Exit(2)
	}

	// 日志级别用变量而不是常量：服务运行中 .log 可以把它调高。
	// 有了这一点，排查不声不响的故障时，就不必重启账号再守着它复现。
	level := new(slog.LevelVar)
	if *verbose {
		level.Set(slog.LevelDebug)
	}
	logs := logtail.New()
	logger := slog.New(logtail.Wrap(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}), logs))

	// --check 只读，不提供服务。要是它也去拿单实例锁，就恰好会在服务
	// 运行时失败，而自更新正是在这个时候拿它检查刚下载的新版本。
	options := app.Options{Root: *root, Version: version, Logger: logger, Debug: *verbose,
		Logs: logs, Level: level, Register: commands.RegisterAll, Configure: extensions.Register, ReadOnly: *check}
	failures := 0
	if *check2 {
		options.AfterReady = func(ctx context.Context, a *app.App, client *bot.Client) error {
			count, err := verify.Run(ctx, client, prefixOf(options), os.Stdout, verify.Cases, a.DispatchMessage)
			failures = count
			return err
		}
	}
	current, err := app.Prepare(ctx, options)
	if err != nil {
		logger.Error("startup.failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer current.Close()

	if *check {
		memory := sysinfo.Read()
		logger.Info("check.ok", slog.String("version", displayVersion()), slog.Int("commands", len(current.Registry.Commands())),
			slog.Float64("rss_mb", sysinfo.Megabytes(memory.RSS)), slog.Float64("heap_mb", sysinfo.Megabytes(memory.HeapAlloc)))
		return
	}
	if err := current.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		logger.Error("runtime.failed", slog.String("error", err.Error()))
		current.Close()
		os.Exit(1)
	}
	if current.RestartRequested() {
		current.Close()
		os.Exit(platform.RestartExitCode)
	}
	if failures > 0 {
		current.Close()
		os.Exit(1)
	}
}

// prefixOf 读出部署所用的命令前缀，供 --verify 使用。
func prefixOf(options app.Options) string {
	return config.ReadEnv(options.Root, os.Environ()).Prefixes()[0]
}

func displayVersion() string {
	if version == "" {
		return "dev"
	}
	return version
}

// restore 把备份解包到 root。
//
// 它和运行服务时拿的是同一把锁。运行中的服务在内存里持有会话，
// 还会随时把状态写回磁盘；这时从底下改写 config.json，两边对这个
// 目录属于哪个账号的认识就会对不上。
func restore(file, root string, overwrite bool) error {
	archive, err := os.Open(file)
	if err != nil {
		return err
	}
	defer archive.Close()
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	lock, err := app.LockRoot(root)
	if errors.Is(err, app.ErrRunning) {
		return errors.New("the service is running on " + root + "; stop it first (systemctl stop laowangbot)")
	}
	if err != nil {
		return err
	}
	defer lock.Close()
	names, err := backup.Restore(archive, root, overwrite)
	if errors.Is(err, backup.ErrExists) {
		return errors.New(root + " already has an account; add --force to replace it with the backup")
	}
	if err != nil {
		return err
	}
	fmt.Printf("Restored %d files into %s:\n", len(names), root)
	for _, name := range names {
		fmt.Println("  " + name)
	}
	return nil
}
