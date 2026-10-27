package channel

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/richardcase/satchel/internal/discover"
	"github.com/richardcase/satchel/internal/gitx"
	"github.com/richardcase/satchel/internal/source"
	"github.com/richardcase/satchel/internal/state"
	"github.com/richardcase/satchel/internal/store"
	"github.com/richardcase/satchel/internal/target"
	"github.com/richardcase/satchel/internal/testrepo"
)

func skill(rel, name, desc string) discover.Skill {
	return discover.Skill{Meta: discover.Meta{Name: name, Description: desc}, Dir: "/rev/" + rel, Rel: rel}
}

func names(sels []selection) []string {
	out := make([]string, len(sels))
	for i, s := range sels {
		out[i] = s.name
	}
	return out
}

func TestResolveNamesUsesFrontmatter(t *testing.T) {
	got, err := resolveNames([]discover.Skill{
		skill("skills/alpha", "alpha-skill", ""),
		skill("skills/beta", "beta-skill", ""),
	}, "repo")
	if err != nil {
		t.Fatalf("resolveNames: %v", err)
	}
	if want := []string{"alpha-skill", "beta-skill"}; strings.Join(names(got), ",") != strings.Join(want, ",") {
		t.Errorf("names = %v, want %v", names(got), want)
	}
}

func TestResolveNamesFallsBackToTheSourceForARootSkill(t *testing.T) {
	got, err := resolveNames([]discover.Skill{skill(".", "", "")}, "my-repo")
	if err != nil {
		t.Fatalf("resolveNames: %v", err)
	}
	if names(got)[0] != "my-repo" {
		t.Errorf("name = %q, want my-repo: a nameless root skill falls back to the source", names(got)[0])
	}
}

func TestResolveNamesFallsBackToTheDirectoryName(t *testing.T) {
	got, err := resolveNames([]discover.Skill{
		skill("skills/alpha", "", ""),
		skill("skills/beta", "beta-skill", ""),
	}, "repo")
	if err != nil {
		t.Fatalf("resolveNames: %v", err)
	}
	if names(got)[0] != "alpha" {
		t.Errorf("name = %q, want alpha: a nameless nested skill falls back to its directory", names(got)[0])
	}
}

func TestResolveNamesRejectsDuplicates(t *testing.T) {
	_, err := resolveNames([]discover.Skill{
		skill("skills/alpha", "same", ""),
		skill("skills/beta", "same", ""),
	}, "repo")
	if err == nil {
		t.Fatal("resolveNames accepted two skills with one name; want an error")
	}
	for _, want := range []string{"skills/alpha", "skills/beta", "same"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %v, want it to mention %q", err, want)
		}
	}
}

func TestResolveNamesRejectsAnUnusableName(t *testing.T) {
	_, err := resolveNames([]discover.Skill{skill("skills/alpha", "../escape", "")}, "repo")
	if err == nil {
		t.Fatal("resolveNames accepted a name with a separator; want an error")
	}
	if !strings.Contains(err.Error(), "--as") {
		t.Errorf("error = %v, want it to suggest --as", err)
	}
}

func TestPickSkillsMatchesNameThenPath(t *testing.T) {
	all, err := resolveNames([]discover.Skill{
		skill("skills/alpha", "alpha-skill", ""),
		skill("skills/beta", "beta-skill", ""),
	}, "repo")
	if err != nil {
		t.Fatal(err)
	}

	got, err := pickSkills(all, []string{"beta-skill", "skills/alpha"})
	if err != nil {
		t.Fatalf("pickSkills: %v", err)
	}
	if want := "beta-skill,alpha-skill"; strings.Join(names(got), ",") != want {
		t.Errorf("names = %v, want %s", names(got), want)
	}
}

func TestPickSkillsIgnoresARepeatedRequest(t *testing.T) {
	all, err := resolveNames([]discover.Skill{skill("skills/alpha", "alpha-skill", "")}, "repo")
	if err != nil {
		t.Fatal(err)
	}

	got, err := pickSkills(all, []string{"alpha-skill", "skills/alpha"})
	if err != nil {
		t.Fatalf("pickSkills: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("pickSkills = %v, want one entry: the same skill asked for twice installs once", names(got))
	}
}

func TestPickSkillsRejectsAnUnknownName(t *testing.T) {
	all, err := resolveNames([]discover.Skill{skill("skills/alpha", "alpha-skill", "")}, "repo")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := pickSkills(all, []string{"nope"}); err == nil {
		t.Fatal("pickSkills accepted an unknown name; want an error")
	} else if !strings.Contains(err.Error(), "nope") {
		t.Errorf("error = %v, want it to name the unmatched skill", err)
	}
}

// narrow turns an unmatched --skill and an unguided multi-skill repository into
// the same kind of error, because both are answered the same way: by listing
// what the repository holds.
func TestNarrowReportsAmbiguity(t *testing.T) {
	all, err := resolveNames([]discover.Skill{
		skill("skills/alpha", "alpha-skill", ""),
		skill("skills/beta", "beta-skill", ""),
	}, "repo")
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name string
		req  Request
		want string
	}{
		{name: "no selector", req: Request{}, want: "--all"},
		{name: "unmatched --skill", req: Request{Skills: []string{"nope"}}, want: "nope"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := narrow(all, tc.req)
			var amb *Ambiguous
			if !errors.As(err, &amb) {
				t.Fatalf("narrow returned %v, want an *Ambiguous the caller can list from", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

func TestNarrowTakesTheOnlySkillWithoutBeingAsked(t *testing.T) {
	all, err := resolveNames([]discover.Skill{skill(".", "solo", "")}, "repo")
	if err != nil {
		t.Fatal(err)
	}

	got, err := narrow(all, Request{})
	if err != nil {
		t.Fatalf("narrow: %v", err)
	}
	if len(got) != 1 || got[0].name != "solo" {
		t.Errorf("narrow = %v, want the single skill chosen without --skill", names(got))
	}
}

func TestSlugFallsBackToTheSource(t *testing.T) {
	src, err := source.Parse("owner/repo")
	if err != nil {
		t.Fatal(err)
	}

	got, err := slugFor(&state.Receipt{Source: src.RepoURL})
	if err != nil {
		t.Fatalf("slugFor: %v", err)
	}
	if got != src.Slug() {
		t.Errorf("slugFor = %q, want %q", got, src.Slug())
	}
}

func TestSlugPrefersTheRecordedOne(t *testing.T) {
	got, err := slugFor(&state.Receipt{Slug: "recorded/slug", Source: "https://example.com/o/r.git"})
	if err != nil {
		t.Fatalf("slugFor: %v", err)
	}
	if got != "recorded/slug" {
		t.Errorf("slugFor = %q, want the recorded slug", got)
	}
}

func TestBriefCarriesNameAndDescription(t *testing.T) {
	all, err := resolveNames([]discover.Skill{skill("skills/alpha", "alpha-skill", "Does the alpha thing")}, "repo")
	if err != nil {
		t.Fatal(err)
	}

	got := brief(all)
	if len(got) != 1 || got[0].Name != "alpha-skill" || got[0].Desc != "Does the alpha thing" {
		t.Errorf("brief = %+v, want the name and description a listing needs", got)
	}
}

func TestBriefCarriesPlugin(t *testing.T) {
	s := skill("plugins/show-me/skills/show-me", "show-me", "")
	s.Plugin = "show-me"
	all, err := resolveNames([]discover.Skill{s}, "repo")
	if err != nil {
		t.Fatal(err)
	}

	got := brief(all)
	if len(got) != 1 || got[0].Plugin != "show-me" {
		t.Errorf("brief = %+v, want Plugin %q carried through", got, "show-me")
	}
}

func TestBriefSortsCandidatesByName(t *testing.T) {
	all, err := resolveNames([]discover.Skill{
		skill("skills/zebra", "zebra-skill", ""),
		skill("skills/monkey", "monkey-skill", ""),
		skill("skills/alpha", "alpha-skill", ""),
	}, "repo")
	if err != nil {
		t.Fatal(err)
	}

	got := brief(all)
	want := []string{"alpha-skill", "monkey-skill", "zebra-skill"}
	gotNames := make([]string, len(got))
	for i, c := range got {
		gotNames[i] = c.Name
	}
	if strings.Join(gotNames, ",") != strings.Join(want, ",") {
		t.Errorf("brief order = %v, want %v: a picker list should not depend on filesystem walk order", gotNames, want)
	}
}

func TestAgentWarningsFlagsAnUndeclaredTarget(t *testing.T) {
	sels := []selection{
		{name: "demo", skill: discover.Skill{Meta: discover.Meta{Agents: []string{"claude"}}}},
	}
	targets := []target.Target{{Name: "claude"}, {Name: "codex"}}

	got := agentWarnings(sels, targets)
	if len(got) != 1 {
		t.Fatalf("got %d warnings, want 1: %v", len(got), got)
	}
	if !strings.Contains(got[0], "codex") || !strings.Contains(got[0], "demo") {
		t.Errorf("warning = %q, want it to name the skill and the undeclared agent", got[0])
	}
}

func TestAgentWarningsEmptyWhenUnrestricted(t *testing.T) {
	sels := []selection{{name: "demo", skill: discover.Skill{}}}
	targets := []target.Target{{Name: "claude"}, {Name: "codex"}}

	if got := agentWarnings(sels, targets); len(got) != 0 {
		t.Errorf("got %v, want no warnings for a skill with no agents declared", got)
	}
}

func TestAgentWarningsEmptyWhenEveryTargetIsDeclared(t *testing.T) {
	sels := []selection{
		{name: "demo", skill: discover.Skill{Meta: discover.Meta{Agents: []string{"claude", "codex"}}}},
	}
	targets := []target.Target{{Name: "claude"}, {Name: "codex"}}

	if got := agentWarnings(sels, targets); len(got) != 0 {
		t.Errorf("got %v, want no warnings when every target is declared", got)
	}
}

// probeGit is the real git with a hook before Resolve, so a test can see what
// was on the progress writer at the moment the slow call began.
type probeGit struct {
	gitx.Git
	onResolve func()
}

func (p *probeGit) Resolve(ctx context.Context, repoURL, ref string) (string, error) {
	if p.onResolve != nil {
		p.onResolve()
	}
	return p.Git.Resolve(ctx, repoURL, ref)
}

// gitProgressFixture is a one-commit repository with its revision already in
// the store, the receipt an install would have written for it, and the parsed
// source, so Prepare and Update can both be driven against it.
func gitProgressFixture(t *testing.T) (c *Git, g *probeGit, r *state.Receipt, src source.Source) {
	t.Helper()

	url, first := testrepo.New(t, map[string]string{"SKILL.md": rollbackSkillMD})
	src, err := source.Parse(url)
	if err != nil {
		t.Fatalf("Parse(%q): %v", url, err)
	}

	st := store.New(t.TempDir())
	g = &probeGit{Git: gitx.New()}
	c = NewGit(st, g)

	rev, err := st.Ensure(context.Background(), g, src.Slug(), src.RepoURL, first)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	hash, err := store.HashDir(rev)
	if err != nil {
		t.Fatalf("HashDir: %v", err)
	}
	r = &state.Receipt{
		Name: "demo", Channel: "git", Source: src.RepoURL, Slug: src.Slug(), Ref: "main",
		Resolved: first, RevPath: rev, ContentHash: hash,
		Links: []state.Link{{Target: "claude", Path: filepath.Join(t.TempDir(), "claude", "demo")}},
	}
	return c, g, r, src
}

func TestGitUpdateReportsEachSkillBeforeResolving(t *testing.T) {
	c, g, r, _ := gitProgressFixture(t)
	var progress bytes.Buffer
	var atResolve string
	g.onResolve = func() { atResolve = progress.String() }

	_, _, err := c.Update(context.Background(), []*state.Receipt{r}, UpdateOptions{Progress: &progress})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if atResolve != "checking demo...\n" {
		t.Errorf("progress when git was asked = %q, want the checking line already written", atResolve)
	}
}

func TestGitUpdateReportsNothingForASkippedPin(t *testing.T) {
	c, _, r, _ := gitProgressFixture(t)
	r.Pinned = true
	var progress bytes.Buffer

	_, _, err := c.Update(context.Background(), []*state.Receipt{r}, UpdateOptions{Named: false, Progress: &progress})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if progress.Len() != 0 {
		t.Errorf("progress = %q, want nothing for a pin update will skip", progress.String())
	}
}

func TestGitPrepareReportsTheRepositoryBeforeResolving(t *testing.T) {
	c, g, _, src := gitProgressFixture(t)
	var progress bytes.Buffer
	var atResolve string
	g.onResolve = func() { atResolve = progress.String() }

	req := Request{Source: src, All: true, Progress: &progress}
	if _, _, err := c.Prepare(context.Background(), req); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if atResolve != "fetching "+src.Redacted()+"...\n" {
		t.Errorf("progress when git was asked = %q, want the fetching line already written", atResolve)
	}
}

// stubGit answers Resolve with a fixed sha and "extracts" a two-skill tree
// without touching a repository, so a Prepare against a URL git could never
// reach (one carrying a token) can still run to its ambiguity report.
type stubGit struct {
	gitx.Git
}

func (stubGit) Resolve(context.Context, string, string) (string, error) {
	return "7c41bf0e1d5855af7b2c4d1e9f0a3b5c6d7e8f90", nil
}

func (stubGit) Mirror(context.Context, string, string) error { return nil }

func (stubGit) Extract(_ context.Context, _, _, dest string) error {
	for _, name := range []string{"alpha", "beta"} {
		dir := filepath.Join(dest, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		body := "---\nname: " + name + "\ndescription: a skill\n---\n"
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func TestGitPrepareAmbiguityHeaderRedactsTheRepositoryURL(t *testing.T) {
	src, err := source.Parse("https://tok@github.com/o/r")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	c := NewGit(store.New(t.TempDir()), stubGit{})

	_, _, err = c.Prepare(context.Background(), Request{Source: src})
	var amb *Ambiguous
	if !errors.As(err, &amb) {
		t.Fatalf("Prepare error = %v, want *Ambiguous for a two-skill repository with nothing chosen", err)
	}
	if strings.Contains(amb.Header, "tok") {
		t.Errorf("header = %q, leaks the token the fetching line was redacted to hide", amb.Header)
	}
	if !strings.Contains(amb.Header, "https://github.com/o/r.git") {
		t.Errorf("header = %q, want the redacted repository URL", amb.Header)
	}
}
