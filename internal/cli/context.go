package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/richardcase/satchel/internal/channel"
	"github.com/richardcase/satchel/internal/claudex"
	"github.com/richardcase/satchel/internal/cosignx"
	"github.com/richardcase/satchel/internal/gitx"
	"github.com/richardcase/satchel/internal/ocix"
	"github.com/richardcase/satchel/internal/prompt"
	"github.com/richardcase/satchel/internal/registry"
	"github.com/richardcase/satchel/internal/state"
	"github.com/richardcase/satchel/internal/store"
	"github.com/richardcase/satchel/internal/target"
)

// newRunner supplies plan.Executor.Run, which is how a plan's Exec ops reach a
// binary. nil means os/exec; tests replace this to keep a unit test from
// shelling out.
var newRunner = func() func(context.Context, []string) error { return nil }

// newPlugins builds the wrapper around the claude binary. Tests replace it, so
// that no test installs a plugin into the developer's own ~/.claude.
var newPlugins = func() claudex.Plugins { return claudex.New() }

// newOCI builds the wrapper around the OCI registry client. Tests replace
// it, so that no test reaches a real registry.
var newOCI = func() ocix.OCI { return ocix.New() }

// newCosign builds the wrapper around the cosign binary. Tests replace it,
// so that no test shells out to a real cosign or reaches a real registry it
// doesn't control.
var newCosign = func() cosignx.Cosign { return cosignx.New() }

// newRegistry builds the client search fetches the skill registry through.
// Tests replace it, so no test reaches the real network. SATCHEL_REGISTRY_URL
// overrides both the config file and the built-in default, mainly so tests
// and self-hosted mirrors do not depend on GitHub.
var newRegistry = func(cfg target.Config, storeRoot string) registry.Registry {
	url := os.Getenv("SATCHEL_REGISTRY_URL")
	if url == "" {
		url = cfg.Registry.URL
	}
	return &registry.HTTP{URL: url, CachePath: filepath.Join(storeRoot, "registry-cache.json")}
}

// newPicker builds the chooser an install falls back to when it cannot tell
// which skill was meant. Tests replace it, so that no test blocks reading a
// terminal that is not there.
//
// It draws on stderr rather than stdout for the same reason cobra's Println
// does: `satchel install repo > log` is still a question worth asking, and
// stdout belongs to whatever the command was piped into.
var newPicker = func() picker { return prompt.Terminal{In: os.Stdin, Out: os.Stderr} }

// picker asks the user to choose from a list of rows. It is an interface here
// rather than a concrete type so a test can answer without a terminal.
type picker interface {
	// Interactive reports whether there is anyone to ask.
	Interactive() bool
	// Select returns the indices chosen, or prompt.ErrCancelled.
	Select(prompt.Options) ([]int, error)
}

// env is the resolved environment a command runs against.
type env struct {
	store *store.Store
	cfg   target.Config
}

// legacyEnvHint prints a one-time notice when oldName (a pre-rename
// SKILLSCTL_* variable) is set but satchel no longer reads it. Detect-and-warn
// only: the old value is never read for anything but this message.
func legacyEnvHint(newName, oldName string) {
	if os.Getenv(newName) == "" && os.Getenv(oldName) != "" {
		fmt.Fprintf(os.Stderr, "satchel: %s is set but no longer used; rename it to %s\n", oldName, newName)
	}
}

func newEnv() (*env, error) {
	root, err := store.Home()
	if err != nil {
		return nil, err
	}
	migrated, err := store.MigrateHome(context.Background(), root)
	if err != nil {
		return nil, err
	}
	if migrated {
		fmt.Fprintf(os.Stderr, "satchel: migrated the skill store to %s\n", root)
	}
	legacyEnvHint("SATCHEL_HOME", "SKILLSCTL_HOME")

	cfgPath, err := target.ConfigPath()
	if err != nil {
		return nil, err
	}
	cfgMigrated, err := target.MigrateConfig(cfgPath)
	if err != nil {
		return nil, err
	}
	if cfgMigrated {
		fmt.Fprintf(os.Stderr, "satchel: migrated the config file to %s\n", cfgPath)
	}
	legacyEnvHint("SATCHEL_CONFIG", "SKILLSCTL_CONFIG")
	legacyEnvHint("SATCHEL_REGISTRY_URL", "SKILLSCTL_REGISTRY_URL")

	cfg, err := target.Load(cfgPath)
	if err != nil {
		return nil, err
	}
	return &env{store: store.New(root), cfg: cfg}, nil
}

// targets resolves the -a flag, defaulting to every present agent. It never
// prompts, which is what makes it safe for new.go's dry-run validation: that
// command doesn't act on the result, so asking would interrupt scaffolding
// for no operational effect.
func (e *env) targets(names []string) ([]target.Target, error) {
	return e.cfg.Resolve(names)
}

// resolveTargets is targets, plus a prompt: install, link and adopt use it
// because they actually act on the result, so a bare invocation deserves a
// visible choice rather than a silent "every present agent". -a still bypasses
// it outright, and a non-interactive caller (no terminal — scripts, CI) falls
// back to targets' old behaviour so nothing there breaks.
func (e *env) resolveTargets(names []string) ([]target.Target, error) {
	if len(names) > 0 {
		return e.cfg.Select(names)
	}
	if p := newPicker(); p.Interactive() {
		return selectAgents(p, e.cfg)
	}
	return e.targets(names)
}

// openState acquires the receipts database.
func (e *env) openState(ctx context.Context) (*state.Handle, error) {
	return state.Open(ctx, e.store.StatePath(), os.Stderr)
}

// channels is the registry every command dispatches through. It is built per
// command rather than shared, because a channel is bound to the store, the
// config and the binaries the command is running against.
func (e *env) channels() channel.Registry {
	return channel.Registry{
		Git:    channel.NewGit(e.store, gitx.New()),
		Plugin: channel.NewPlugin(newPlugins(), e.cfg),
		Local:  channel.NewLocal(e.store),
		OCI:    channel.NewOCI(e.store, newOCI(), newCosign()),
	}
}

// registry builds the client search fetches the skill registry through, bound
// to this environment's config and store, the same way channels() is bound to
// them.
func (e *env) registry() registry.Registry {
	return newRegistry(e.cfg, e.store.Root)
}
