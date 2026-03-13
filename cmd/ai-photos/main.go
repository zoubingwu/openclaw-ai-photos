package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"syscall"
	"time"

	"ai-photos/internal/app"
)

type commandHelp struct {
	Name      string
	Summary   string
	Usage     string
	Workflow  []string
	Arguments []string
	Output    []string
	Examples  []string
	Notes     []string
}

func main() {
	if len(os.Args) < 2 {
		printGlobalHelp(os.Stderr)
		os.Exit(2)
	}

	command := os.Args[1]
	args := os.Args[2:]

	if command == "help" {
		if err := runHelp(args); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	if command == "-h" || command == "--help" {
		printGlobalHelp(os.Stdout)
		return
	}

	var err error
	switch command {
	case "save-profile":
		err = runSaveProfile(args)
	case "build-manifest":
		err = runBuildManifest(args)
	case "init":
		err = runInit(args)
	case "setup":
		err = runSetup(args)
	case "sync":
		err = runSync(args)
	case "import":
		err = runImport(args)
	case "search":
		err = runSearch(args)
	case "prepare-image":
		err = runPrepareImage(args)
	case "serve":
		err = runServe(args)
	default:
		err = fmt.Errorf("unknown subcommand %q\n\nRun `ai-photos help` to see the available commands", command)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runHelp(args []string) error {
	if len(args) == 0 {
		printGlobalHelp(os.Stdout)
		return nil
	}
	if len(args) > 1 {
		return fmt.Errorf("usage: ai-photos help [subcommand]")
	}
	if err := printSubcommandHelp(os.Stdout, args[0]); err != nil {
		return err
	}
	return nil
}

func runSaveProfile(args []string) error {
	fs := flag.NewFlagSet("save-profile", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var (
		sources         stringSliceFlag
		backend         string
		target          string
		displayName     string
		maintenanceMode string
	)
	fs.Var(&sources, "source", "Photo source path. Repeat to attach multiple folders to one album.")
	fs.StringVar(&backend, "backend", "", "Backend kind: db9 or tidb.")
	fs.StringVar(&target, "target", "", "db9 database name/id, or path to a TiDB target JSON file.")
	fs.StringVar(&displayName, "display-name", "", "Human-readable album name stored in the profile.")
	fs.StringVar(&maintenanceMode, "maintenance-mode", "heartbeat", "Maintenance mode recorded in the profile.")
	if wantsCommandHelp(args) {
		printCommandHelp(os.Stdout, helpSaveProfile(), fs)
		return nil
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(sources) == 0 || backend == "" || target == "" {
		return fmt.Errorf("usage: ai-photos save-profile [flags] [profile]\n\n--source, --backend, and --target are required")
	}
	profileRef := ""
	if fs.NArg() > 1 {
		return fmt.Errorf("usage: ai-photos save-profile [flags] [profile]\n\nsave-profile accepts at most one profile argument")
	}
	if fs.NArg() == 1 {
		profileRef = fs.Arg(0)
	}
	path, profile, err := app.SaveProfile(profileRef, sources, backend, target, displayName, maintenanceMode)
	if err != nil {
		return err
	}
	return printJSON(map[string]any{
		"ok":           true,
		"profile_path": path,
		"profile":      profile,
	})
}

func runBuildManifest(args []string) error {
	fs := flag.NewFlagSet("build-manifest", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var output string
	fs.StringVar(&output, "o", "", "Output JSONL path.")
	fs.StringVar(&output, "output", "", "Output JSONL path.")
	if wantsCommandHelp(args) {
		printCommandHelp(os.Stdout, helpBuildManifest(), fs)
		return nil
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if output == "" || fs.NArg() == 0 {
		return fmt.Errorf("usage: ai-photos build-manifest -o <output.jsonl> <source> [source...]\n\none or more sources and -o/--output are required")
	}
	summary, err := app.BuildManifest(fs.Args(), output)
	if err != nil {
		return err
	}
	return printJSON(summary)
}

func runInit(args []string) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var (
		backend    string
		profileRef string
	)
	fs.StringVar(&backend, "backend", "", "Backend kind override: db9 or tidb.")
	fs.StringVar(&profileRef, "profile", "", "Profile name or path. If omitted, the default profile is used.")
	if wantsCommandHelp(args) {
		printCommandHelp(os.Stdout, helpInit(), fs)
		return nil
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 1 {
		return fmt.Errorf("usage: ai-photos init [flags] [target]\n\ninit accepts at most one target argument")
	}
	target := ""
	if fs.NArg() == 1 {
		target = fs.Arg(0)
	}
	summary, err := app.InitSchema(context.Background(), target, backend, profileRef)
	if err != nil {
		return err
	}
	return printJSON(summary)
}

func runSetup(args []string) error {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var (
		sources         stringSliceFlag
		backend         string
		target          string
		displayName     string
		maintenanceMode string
		manifestOut     string
	)
	fs.Var(&sources, "source", "Photo source path. Repeat to attach multiple folders to one album.")
	fs.StringVar(&backend, "backend", "", "Backend kind: db9 or tidb.")
	fs.StringVar(&target, "target", "", "db9 database name/id, or path to a TiDB target JSON file.")
	fs.StringVar(&displayName, "display-name", "", "Human-readable album name stored in the profile.")
	fs.StringVar(&maintenanceMode, "maintenance-mode", "heartbeat", "Maintenance mode recorded in the profile.")
	fs.StringVar(&manifestOut, "manifest-out", "", "Optional manifest output path. The incremental manifest will be written beside it.")
	if wantsCommandHelp(args) {
		printCommandHelp(os.Stdout, helpSetup(), fs)
		return nil
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(sources) == 0 || backend == "" || target == "" {
		return fmt.Errorf("usage: ai-photos setup [flags] [profile]\n\n--source, --backend, and --target are required")
	}
	profileRef := ""
	if fs.NArg() > 1 {
		return fmt.Errorf("usage: ai-photos setup [flags] [profile]\n\nsetup accepts at most one profile argument")
	}
	if fs.NArg() == 1 {
		profileRef = fs.Arg(0)
	}
	summary, err := app.SetupAlbum(context.Background(), profileRef, sources, backend, target, displayName, maintenanceMode, manifestOut)
	if err != nil {
		return err
	}
	return printJSON(summary)
}

func runSync(args []string) error {
	fs := flag.NewFlagSet("sync", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var (
		backend     string
		profileRef  string
		manifestOut string
	)
	fs.StringVar(&backend, "backend", "", "Backend kind override: db9 or tidb.")
	fs.StringVar(&profileRef, "profile", "", "Profile name or path. If omitted, the default profile is used.")
	fs.StringVar(&manifestOut, "manifest-out", "", "Optional manifest output path. The incremental manifest will be written beside it.")
	if wantsCommandHelp(args) {
		printCommandHelp(os.Stdout, helpSync(), fs)
		return nil
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	target := ""
	sources := []string{}
	remaining := fs.Args()
	if len(remaining) > 0 {
		target = remaining[0]
		sources = remaining[1:]
	}
	summary, err := app.SyncPhotos(context.Background(), target, backend, sources, profileRef, manifestOut)
	if err != nil {
		return err
	}
	return printJSON(summary)
}

func runImport(args []string) error {
	fs := flag.NewFlagSet("import", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var (
		backend    string
		profileRef string
	)
	fs.StringVar(&backend, "backend", "", "Backend kind override: db9 or tidb.")
	fs.StringVar(&profileRef, "profile", "", "Profile name or path. If omitted, the default profile is used.")
	if wantsCommandHelp(args) {
		printCommandHelp(os.Stdout, helpImport(), fs)
		return nil
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() == 0 || fs.NArg() > 2 {
		return fmt.Errorf("usage: ai-photos import [flags] [target] <captioned.jsonl>\n\nimport expects a captioned JSONL path and an optional target")
	}
	target := ""
	jsonlPath := ""
	if fs.NArg() == 1 {
		jsonlPath = fs.Arg(0)
	} else {
		target = fs.Arg(0)
		jsonlPath = fs.Arg(1)
	}
	summary, err := app.ImportRecords(context.Background(), target, jsonlPath, backend, profileRef)
	if err != nil {
		return err
	}
	return printJSON(summary)
}

func runSearch(args []string) error {
	fs := flag.NewFlagSet("search", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var (
		backend    string
		profileRef string
		date       string
		text       string
		tag        string
		recent     bool
		limit      int
	)
	fs.StringVar(&backend, "backend", "", "Backend kind override: db9 or tidb.")
	fs.StringVar(&profileRef, "profile", "", "Profile name or path. If omitted, the default profile is used.")
	fs.StringVar(&date, "date", "", "Date prefix filter, for example 2026-03 or 2026-03-13.")
	fs.StringVar(&text, "text", "", "Free-text query against caption and retrieval text.")
	fs.StringVar(&tag, "tag", "", "Single tag filter.")
	fs.BoolVar(&recent, "recent", false, "List most recently indexed photos. Cannot be combined with --text, --tag, or --date.")
	fs.IntVar(&limit, "limit", 20, "Maximum number of results to return.")
	if wantsCommandHelp(args) {
		printCommandHelp(os.Stdout, helpSearch(), fs)
		return nil
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 1 {
		return fmt.Errorf("usage: ai-photos search [flags] [target]\n\nsearch accepts at most one target argument")
	}
	target := ""
	if fs.NArg() == 1 {
		target = fs.Arg(0)
	}
	resolvedBackend, resolvedTarget, _, err := app.ResolveBackendTarget(target, backend, profileRef)
	if err != nil {
		return err
	}
	store, err := app.OpenBackend(resolvedBackend, resolvedTarget)
	if err != nil {
		return err
	}
	result, err := store.Search(context.Background(), app.SearchParams{
		Text:     text,
		Tag:      tag,
		Date:     date,
		Recent:   recent,
		Page:     1,
		PageSize: limit,
	})
	if err != nil {
		return err
	}
	return printJSON(map[string]any{
		"ok":        true,
		"backend":   resolvedBackend,
		"target":    resolvedTarget,
		"items":     result.Items,
		"page":      result.Page,
		"page_size": result.PageSize,
		"total":     result.Total,
		"has_more":  result.HasMore,
	})
}

func runPrepareImage(args []string) error {
	fs := flag.NewFlagSet("prepare-image", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var mode string
	fs.StringVar(&mode, "mode", "", "Preparation mode: caption or preview.")
	if wantsCommandHelp(args) {
		printCommandHelp(os.Stdout, helpPrepareImage(), fs)
		return nil
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 || mode == "" {
		return fmt.Errorf("usage: ai-photos prepare-image --mode <caption|preview> <image>\n\nprepare-image expects one image path and --mode")
	}
	var spec app.PrepareSpec
	switch mode {
	case "caption":
		spec = app.CaptionPrepareSpec()
	case "preview":
		spec = app.PreviewPrepareSpec()
	default:
		return fmt.Errorf("unsupported mode %q", mode)
	}
	result, err := app.PrepareImage(fs.Arg(0), spec)
	if err != nil {
		return err
	}
	return printJSON(result)
}

func runServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var opts app.Options
	fs.StringVar(&opts.ProfileRef, "profile", "", "Profile name or path. If omitted, the default profile is used.")
	fs.StringVar(&opts.Host, "host", "127.0.0.1", "Listen host.")
	fs.IntVar(&opts.Port, "port", 0, "Listen port. Use 0 to pick a free local port.")
	fs.StringVar(&opts.CacheDir, "cache-dir", "", "Cache directory for thumbnails and preview images.")
	fs.BoolVar(&opts.OpenBrowser, "open-browser", false, "Open the local URL in the default browser after startup.")
	if wantsCommandHelp(args) {
		printCommandHelp(os.Stdout, helpServe(), fs)
		return nil
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := app.LoadConfig(opts)
	if err != nil {
		return err
	}
	server, err := app.NewServer(cfg)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := server.CheckReady(ctx); err != nil {
		return err
	}

	listener, err := net.Listen("tcp", net.JoinHostPort(cfg.Host, cfg.PortString()))
	if err != nil {
		return err
	}
	defer listener.Close()

	url := app.LocalURL(listener.Addr())
	startup := map[string]any{
		"type":         "server-started",
		"host":         cfg.Host,
		"port":         app.PortFromAddr(listener.Addr()),
		"url":          url,
		"backend":      cfg.BackendKind,
		"profile_path": cfg.ProfilePath,
		"source":       cfg.ConfigSource,
		"cache_dir":    cfg.CacheDir,
	}
	if err := printJSONLine(startup); err != nil {
		return err
	}

	if cfg.OpenBrowser {
		go func() {
			time.Sleep(250 * time.Millisecond)
			_ = app.OpenBrowser(url)
		}()
	}

	httpServer := &http.Server{
		Handler:           server.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()

	if err := httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

func printGlobalHelp(w io.Writer) {
	fmt.Fprintln(w, "ai-photos")
	fmt.Fprintln(w, "  Turn one or more local photo folders into a searchable AI photo album.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "What This CLI Covers")
	fmt.Fprintln(w, "  1. Create or reconnect an album profile.")
	fmt.Fprintln(w, "  2. Initialize the backend schema.")
	fmt.Fprintln(w, "  3. Scan folders and produce incremental manifests for captioning.")
	fmt.Fprintln(w, "  4. Import captioned records into the album.")
	fmt.Fprintln(w, "  5. Search the album or open a local web UI.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Typical Workflow")
	fmt.Fprintln(w, "  1. Create the album: ai-photos setup --source ~/Pictures --backend db9 --target my_album")
	fmt.Fprintln(w, "  2. Read caption_input_jsonl from the JSON output.")
	fmt.Fprintln(w, "  3. For each file in that manifest, run prepare-image --mode caption, send output_path to a vision model,")
	fmt.Fprintln(w, "     then write a captioned JSONL with caption, tags, scene, objects, and text_in_image.")
	fmt.Fprintln(w, "  4. Import the captioned JSONL: ai-photos import /tmp/photos.captioned.jsonl")
	fmt.Fprintln(w, "  5. Search: ai-photos search --text \"cat on sofa\"")
	fmt.Fprintln(w, "  6. Browse locally: ai-photos serve --open-browser")
	fmt.Fprintln(w, "  7. Later updates: ai-photos sync")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Notes")
	fmt.Fprintln(w, "  - Profiles are stored under ~/.openclaw/ai-photos/albums by default.")
	fmt.Fprintln(w, "  - Automatic indexing is external to this CLI. Schedule `ai-photos sync` yourself if needed.")
	fmt.Fprintln(w, "  - Image preparation uses local tools such as sips or ImageMagick when available.")
	fmt.Fprintln(w, "  - db9 remains an external dependency when you choose the db9 backend.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Commands")
	fmt.Fprintln(w, "  Album setup")
	fmt.Fprintln(w, "    save-profile   Save or update a managed album profile.")
	fmt.Fprintln(w, "    init           Create the photo schema in db9 or TiDB.")
	fmt.Fprintln(w, "    setup          Save a profile, initialize the backend, and build the first incremental manifest.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "  Indexing")
	fmt.Fprintln(w, "    build-manifest Scan folders and write a full manifest JSONL.")
	fmt.Fprintln(w, "    sync           Compare the current folders with the backend and write an incremental manifest JSONL.")
	fmt.Fprintln(w, "    import         Import a captioned JSONL file into the album.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "  Query and media")
	fmt.Fprintln(w, "    search         Query the album by text, tag, date, or recent indexing order.")
	fmt.Fprintln(w, "    prepare-image  Prepare a local image for captioning or preview delivery.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "  Local web")
	fmt.Fprintln(w, "    serve          Start the local browser UI and JSON API.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Examples")
	fmt.Fprintln(w, "  ai-photos help setup")
	fmt.Fprintln(w, "  ai-photos setup --source ~/Pictures --backend db9 --target my_album")
	fmt.Fprintln(w, "  ai-photos sync")
	fmt.Fprintln(w, "  ai-photos import /tmp/photos.captioned.jsonl")
	fmt.Fprintln(w, "  ai-photos search --text \"sunset beach\"")
	fmt.Fprintln(w, "  ai-photos prepare-image --mode preview ~/Pictures/IMG_0001.HEIC")
	fmt.Fprintln(w, "  ai-photos serve --profile default --open-browser")
}

func printSubcommandHelp(w io.Writer, name string) error {
	switch name {
	case "save-profile":
		fs := flag.NewFlagSet("save-profile", flag.ContinueOnError)
		fs.Var(new(stringSliceFlag), "source", "Photo source path. Repeat to attach multiple folders to one album.")
		fs.String("backend", "", "Backend kind: db9 or tidb.")
		fs.String("target", "", "db9 database name/id, or path to a TiDB target JSON file.")
		fs.String("display-name", "", "Human-readable album name stored in the profile.")
		fs.String("maintenance-mode", "heartbeat", "Maintenance mode recorded in the profile.")
		printCommandHelp(w, helpSaveProfile(), fs)
	case "build-manifest":
		fs := flag.NewFlagSet("build-manifest", flag.ContinueOnError)
		fs.String("o", "", "Output JSONL path.")
		fs.String("output", "", "Output JSONL path.")
		printCommandHelp(w, helpBuildManifest(), fs)
	case "init":
		fs := flag.NewFlagSet("init", flag.ContinueOnError)
		fs.String("backend", "", "Backend kind override: db9 or tidb.")
		fs.String("profile", "", "Profile name or path. If omitted, the default profile is used.")
		printCommandHelp(w, helpInit(), fs)
	case "setup":
		fs := flag.NewFlagSet("setup", flag.ContinueOnError)
		fs.Var(new(stringSliceFlag), "source", "Photo source path. Repeat to attach multiple folders to one album.")
		fs.String("backend", "", "Backend kind: db9 or tidb.")
		fs.String("target", "", "db9 database name/id, or path to a TiDB target JSON file.")
		fs.String("display-name", "", "Human-readable album name stored in the profile.")
		fs.String("maintenance-mode", "heartbeat", "Maintenance mode recorded in the profile.")
		fs.String("manifest-out", "", "Optional manifest output path. The incremental manifest will be written beside it.")
		printCommandHelp(w, helpSetup(), fs)
	case "sync":
		fs := flag.NewFlagSet("sync", flag.ContinueOnError)
		fs.String("backend", "", "Backend kind override: db9 or tidb.")
		fs.String("profile", "", "Profile name or path. If omitted, the default profile is used.")
		fs.String("manifest-out", "", "Optional manifest output path. The incremental manifest will be written beside it.")
		printCommandHelp(w, helpSync(), fs)
	case "import":
		fs := flag.NewFlagSet("import", flag.ContinueOnError)
		fs.String("backend", "", "Backend kind override: db9 or tidb.")
		fs.String("profile", "", "Profile name or path. If omitted, the default profile is used.")
		printCommandHelp(w, helpImport(), fs)
	case "search":
		fs := flag.NewFlagSet("search", flag.ContinueOnError)
		fs.String("backend", "", "Backend kind override: db9 or tidb.")
		fs.String("profile", "", "Profile name or path. If omitted, the default profile is used.")
		fs.String("date", "", "Date prefix filter, for example 2026-03 or 2026-03-13.")
		fs.String("text", "", "Free-text query against caption and retrieval text.")
		fs.String("tag", "", "Single tag filter.")
		fs.Bool("recent", false, "List most recently indexed photos.")
		fs.Int("limit", 20, "Maximum number of results to return.")
		printCommandHelp(w, helpSearch(), fs)
	case "prepare-image":
		fs := flag.NewFlagSet("prepare-image", flag.ContinueOnError)
		fs.String("mode", "", "Preparation mode: caption or preview.")
		printCommandHelp(w, helpPrepareImage(), fs)
	case "serve":
		fs := flag.NewFlagSet("serve", flag.ContinueOnError)
		fs.String("profile", "", "Profile name or path. If omitted, the default profile is used.")
		fs.String("host", "127.0.0.1", "Listen host.")
		fs.Int("port", 0, "Listen port. Use 0 to pick a free local port.")
		fs.String("cache-dir", "", "Cache directory for thumbnails and preview images.")
		fs.Bool("open-browser", false, "Open the local URL in the default browser after startup.")
		printCommandHelp(w, helpServe(), fs)
	default:
		return fmt.Errorf("unknown subcommand %q\n\nRun `ai-photos help` to see the available commands", name)
	}
	return nil
}

func printCommandHelp(w io.Writer, help commandHelp, fs *flag.FlagSet) {
	fmt.Fprintf(w, "%s\n", help.Name)
	fmt.Fprintf(w, "  %s\n", help.Summary)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage")
	fmt.Fprintf(w, "  %s\n", help.Usage)
	if len(help.Workflow) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "How To Use It")
		for _, line := range help.Workflow {
			fmt.Fprintf(w, "  - %s\n", line)
		}
	}
	if len(help.Arguments) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Arguments")
		for _, line := range help.Arguments {
			fmt.Fprintf(w, "  - %s\n", line)
		}
	}
	if fs != nil && hasRegisteredFlags(fs) {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Flags")
		printFlagDefaults(w, fs)
	}
	if len(help.Output) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Output")
		for _, line := range help.Output {
			fmt.Fprintf(w, "  - %s\n", line)
		}
	}
	if len(help.Notes) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Notes")
		for _, line := range help.Notes {
			fmt.Fprintf(w, "  - %s\n", line)
		}
	}
	if len(help.Examples) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Examples")
		for _, line := range help.Examples {
			fmt.Fprintf(w, "  %s\n", line)
		}
	}
}

func printFlagDefaults(w io.Writer, fs *flag.FlagSet) {
	names := make([]string, 0)
	fs.VisitAll(func(f *flag.Flag) {
		names = append(names, f.Name)
	})
	sort.Strings(names)
	for _, name := range names {
		f := fs.Lookup(name)
		defaultText := ""
		if f.DefValue != "" && f.DefValue != "false" && f.DefValue != "[]" {
			defaultText = fmt.Sprintf(" (default %q)", f.DefValue)
		}
		fmt.Fprintf(w, "  -%s\n      %s%s\n", f.Name, f.Usage, defaultText)
	}
}

func hasRegisteredFlags(fs *flag.FlagSet) bool {
	hasFlags := false
	fs.VisitAll(func(_ *flag.Flag) {
		hasFlags = true
	})
	return hasFlags
}

func helpSaveProfile() commandHelp {
	return commandHelp{
		Name:    "save-profile",
		Summary: "Save or update a managed album profile without touching the backend schema.",
		Usage:   "ai-photos save-profile [flags] [profile]",
		Workflow: []string{
			"Use this when you want to create or update reconnect information only.",
			"Profiles let later commands resolve sources and backend details automatically.",
			"The optional [profile] can be a simple name like `default` or a JSON path.",
		},
		Arguments: []string{
			"[profile]: Optional profile name or profile JSON path. If omitted, the managed default profile is used.",
		},
		Output: []string{
			"`profile_path`: where the profile JSON was written.",
			"`profile`: the saved profile content, including normalized sources and backend settings.",
		},
		Examples: []string{
			"ai-photos save-profile --source ~/Pictures --backend db9 --target my_album",
			"ai-photos save-profile --source ~/Pictures --source ~/Phone --backend tidb --target ~/tidb-target.json family",
		},
	}
}

func helpBuildManifest() commandHelp {
	return commandHelp{
		Name:    "build-manifest",
		Summary: "Scan one or more source folders and write a full manifest JSONL.",
		Usage:   "ai-photos build-manifest -o <output.jsonl> <source> [source...]",
		Workflow: []string{
			"Use this for manual debugging or when you want a full file inventory before comparing against a backend.",
			"Each line contains file metadata such as file_path, sha256, dimensions, and taken_at when available.",
		},
		Arguments: []string{
			"<source>: One or more local photo folders.",
		},
		Output: []string{
			"`output`: the JSONL path that was written.",
			"`count`: number of supported image files scanned.",
		},
		Examples: []string{
			"ai-photos build-manifest -o /tmp/photos.manifest.jsonl ~/Pictures",
			"ai-photos build-manifest --output /tmp/photos.manifest.jsonl ~/Pictures ~/Phone",
		},
	}
}

func helpInit() commandHelp {
	return commandHelp{
		Name:    "init",
		Summary: "Create the photo schema in db9 or TiDB.",
		Usage:   "ai-photos init [flags] [target]",
		Workflow: []string{
			"Use this when the backend exists but the `photos` table and indexes do not.",
			"If target is omitted, init resolves the backend from the selected profile or the default profile.",
		},
		Arguments: []string{
			"[target]: Optional db9 database name/id or TiDB target JSON path.",
		},
		Output: []string{
			"`backend`: the resolved backend kind.",
			"`target`: the resolved backend target.",
		},
		Examples: []string{
			"ai-photos init --profile default",
			"ai-photos init --backend db9 my_album",
			"ai-photos init --backend tidb ~/tidb-target.json",
		},
	}
}

func helpSetup() commandHelp {
	return commandHelp{
		Name:    "setup",
		Summary: "Create an album profile, initialize the backend schema, and build the first incremental manifest.",
		Usage:   "ai-photos setup [flags] [profile]",
		Workflow: []string{
			"This is the main entrypoint for a new album.",
			"After setup, read `caption_input_jsonl` from the output and run your vision captioning pass.",
			"Once your captioned JSONL is ready, import it with `ai-photos import`.",
		},
		Arguments: []string{
			"[profile]: Optional profile name or profile JSON path. If omitted, the managed default profile is used.",
		},
		Output: []string{
			"`profile_path`: where the profile JSON was saved.",
			"`caption_input_jsonl`: the first manifest that still needs captioning.",
			"`sync.to_caption`: how many records are still waiting to be captioned and imported.",
			"`next_step`: a machine-readable reminder of the next ingestion step.",
		},
		Notes: []string{
			"Setup does not call a vision model. You still need to caption `caption_input_jsonl` and import the result.",
		},
		Examples: []string{
			"ai-photos setup --source ~/Pictures --backend db9 --target my_album",
			"ai-photos setup --source ~/Pictures --source ~/Phone --backend tidb --target ~/tidb-target.json family",
		},
	}
}

func helpSync() commandHelp {
	return commandHelp{
		Name:    "sync",
		Summary: "Scan the current photo folders, compare them with the backend, and write the next incremental manifest.",
		Usage:   "ai-photos sync [flags] [target] [source...]",
		Workflow: []string{
			"Use this after the first import, or on a schedule, to detect new or changed files.",
			"If `to_caption` is 0, the backend already matches the current folders.",
			"If `to_caption` is greater than 0, caption `incremental_manifest_jsonl` and then import it.",
		},
		Arguments: []string{
			"[target]: Optional db9 database name/id or TiDB target JSON path.",
			"[source...]: Optional source folders. When a profile is used, they must match the profile sources.",
		},
		Output: []string{
			"`incremental_manifest_jsonl`: the JSONL file that still needs captioning.",
			"`to_caption`: number of changed or new records.",
			"`backend_status`: whether the comparison used the backend successfully or fell back to a full scan.",
		},
		Examples: []string{
			"ai-photos sync",
			"ai-photos sync --profile default",
			"ai-photos sync --backend db9 my_album ~/Pictures",
		},
	}
}

func helpImport() commandHelp {
	return commandHelp{
		Name:    "import",
		Summary: "Import a captioned JSONL file into the album backend with upsert behavior.",
		Usage:   "ai-photos import [flags] [target] <captioned.jsonl>",
		Workflow: []string{
			"Run this after your vision model has turned a manifest into captioned JSONL records.",
			"Each record should preserve the original file metadata and add caption, tags, scene, objects, and text_in_image.",
		},
		Arguments: []string{
			"[target]: Optional db9 database name/id or TiDB target JSON path.",
			"<captioned.jsonl>: JSONL file to import.",
		},
		Output: []string{
			"`imported`: number of JSONL records written into the backend.",
			"`backend`: the resolved backend kind.",
		},
		Notes: []string{
			"The importer upserts by `file_path`, so rerunning it updates existing rows instead of duplicating them.",
		},
		Examples: []string{
			"ai-photos import /tmp/photos.captioned.jsonl",
			"ai-photos import --profile default /tmp/photos.captioned.jsonl",
			"ai-photos import --backend tidb ~/tidb-target.json /tmp/photos.captioned.jsonl",
		},
	}
}

func helpSearch() commandHelp {
	return commandHelp{
		Name:    "search",
		Summary: "Query the album by text, tag, date prefix, or recent indexing order.",
		Usage:   "ai-photos search [flags] [target]",
		Workflow: []string{
			"Use `--text` for caption and retrieval text searches.",
			"Use `--tag` for exact tag filters.",
			"Use `--date` for prefix matching such as a month or day.",
			"Use `--recent` to list the latest indexed items instead of applying other filters.",
		},
		Arguments: []string{
			"[target]: Optional db9 database name/id or TiDB target JSON path.",
		},
		Output: []string{
			"`items`: photo summaries with id, file_path, filename, caption, taken_at, tags, width, and height.",
			"`total`: total matching rows before the limit is applied.",
			"`has_more`: whether more rows exist beyond this page.",
		},
		Examples: []string{
			"ai-photos search --text \"cat on sofa\"",
			"ai-photos search --tag cat",
			"ai-photos search --date 2026-03",
			"ai-photos search --recent --limit 10",
		},
	}
}

func helpPrepareImage() commandHelp {
	return commandHelp{
		Name:    "prepare-image",
		Summary: "Prepare a local image for captioning or preview delivery.",
		Usage:   "ai-photos prepare-image --mode <caption|preview> <image>",
		Workflow: []string{
			"`caption` mode prepares a smaller image for sending to a vision model.",
			"`preview` mode prepares a browser-friendly local preview image.",
			"If the source is already small enough, the command can return the original file path.",
		},
		Arguments: []string{
			"<image>: Local image file path.",
		},
		Output: []string{
			"`output_path`: file path you should use next.",
			"`used_original`: whether no derived image was needed.",
			"`backend`: the local image backend used, such as sips, magick, imagemagick, direct, or original.",
		},
		Notes: []string{
			"In caption mode, jpg/jpeg/png/webp can fall back to the original file when no local image backend is available.",
		},
		Examples: []string{
			"ai-photos prepare-image --mode caption ~/Pictures/IMG_0001.HEIC",
			"ai-photos prepare-image --mode preview ~/Pictures/IMG_0001.JPG",
		},
	}
}

func helpServe() commandHelp {
	return commandHelp{
		Name:    "serve",
		Summary: "Start the local browser UI and JSON API for browsing the album.",
		Usage:   "ai-photos serve [flags]",
		Workflow: []string{
			"The command resolves the backend from the selected profile or environment variables.",
			"On startup it prints one JSON line with the local URL, backend, resolved profile path, and cache directory.",
			"Keep the process running while the browser UI is in use.",
		},
		Output: []string{
			"Startup writes a single JSON object with `url`, `backend`, `profile_path`, `source`, and `cache_dir`.",
			"The running server exposes health, search, detail, thumbnail, preview, and open-file endpoints.",
		},
		Examples: []string{
			"ai-photos serve",
			"ai-photos serve --profile default",
			"ai-photos serve --port 8080 --open-browser",
		},
	}
}

func wantsCommandHelp(args []string) bool {
	for _, arg := range args {
		if arg == "-h" || arg == "--help" || arg == "-help" {
			return true
		}
	}
	return false
}

func printJSON(value any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func printJSONLine(value any) error {
	return json.NewEncoder(os.Stdout).Encode(value)
}

type stringSliceFlag []string

func (s *stringSliceFlag) String() string {
	return fmt.Sprint([]string(*s))
}

func (s *stringSliceFlag) Set(value string) error {
	*s = append(*s, value)
	return nil
}

func init() {
	log.SetFlags(0)
}
