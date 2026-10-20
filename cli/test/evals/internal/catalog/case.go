// Package catalog loads the eval cases (cases/<suite>/<id>.yaml), the presets
// that select them, and the model configuration. It owns the case schema; every
// other package reads cases through these types.
package catalog

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Suites, in report order.
var Suites = []string{"perf"}

// Arms. An arm fixes the surface the agent works through and what it is given.
const (
	ArmCLISkill   = "cli+skill"   // traceway CLI in the shell, traceway skill available
	ArmCLINoSkill = "cli-noskill" // traceway CLI in the shell, no skills (baseline)
)

// AllArms lists every arm the runner knows.
var AllArms = []string{ArmCLISkill, ArmCLINoSkill}

// Case is one eval scenario. Strings in Prompt, Expect, Oracle and Hooks may
// contain {{seed.x}} / {{env.x}} placeholders, resolved after seeding.
type Case struct {
	ID         string   `yaml:"id"`
	Suite      string   `yaml:"suite"`
	Title      string   `yaml:"title"`
	Tags       []string `yaml:"tags"`
	Seed       string   `yaml:"seed"`
	Arms       []string `yaml:"arms"`
	Invoke     string   `yaml:"invoke"` // slash | natural
	Distractor string   `yaml:"distractor"`

	Workspace   Workspace         `yaml:"workspace"`
	Prompt      string            `yaml:"prompt"`
	Budget      Budget            `yaml:"budget"`
	User        []UserRule        `yaml:"user"`
	Expect      Expect            `yaml:"expect"`
	Judge       JudgeSpec         `yaml:"judge"`
	Oracle      []Step            `yaml:"oracle"`
	OracleByArm map[string][]Step `yaml:"oracle_by_arm"`

	// Path is the YAML file the case was loaded from.
	Path string `yaml:"-"`
}

// Workspace names the fixture repo copied into the agent's work dir.
type Workspace struct {
	Repo string `yaml:"repo"`
}

// Budget bounds one run. Zero values take the defaults in Defaults().
type Budget struct {
	MaxTurns        int      `yaml:"max_turns"`
	MaxToolCalls    int      `yaml:"max_tool_calls"`
	MaxOutputTokens int      `yaml:"max_output_tokens"`
	Timeout         Duration `yaml:"timeout"`
}

// Defaults fills unset budget fields.
func (b Budget) Defaults() Budget {
	if b.MaxTurns == 0 {
		b.MaxTurns = 40
	}
	if b.MaxToolCalls == 0 {
		b.MaxToolCalls = 30
	}
	if b.MaxOutputTokens == 0 {
		b.MaxOutputTokens = 12000
	}
	if b.Timeout == 0 {
		b.Timeout = Duration(10 * time.Minute)
	}
	return b
}

// UserRule scripts one ask_user answer. The first rule whose Match regexp
// matches the question wins; a rule with Default set matches anything.
type UserRule struct {
	Match   string `yaml:"match"`
	Reply   string `yaml:"reply"`
	Default string `yaml:"default"`
	// Once makes the rule apply to its first match only.
	Once bool `yaml:"once"`
}

// Expect is everything the deterministic graders check.
type Expect struct {
	Answer    *Answer     `yaml:"answer"`
	Grounding []string    `yaml:"grounding"`
	Calls     *Trajectory `yaml:"calls"`
	// Report holds regexps the final report text must (or must not) match.
	Report *ReportSpec `yaml:"report"`
}

// ReportSpec checks the final free-text report.
type ReportSpec struct {
	Contains    []string `yaml:"contains"`
	NotContains []string `yaml:"not_contains"`
}

// Answer is the expected structured answer to submit_perf_diagnosis.
type Answer struct {
	ChecklistItem string   `yaml:"checklist_item"`
	AnyOf         []string `yaml:"any_of"`
	Not           []string `yaml:"not"`
	Locus         *Locus   `yaml:"locus"`
	Onset         *Onset   `yaml:"onset"`
	// MentionsAll lists regexps that must all match somewhere in the answer.
	MentionsAll []string `yaml:"mentions_all"`
}

// Locus pins where the bottleneck is. Every set field must match.
type Locus struct {
	Endpoint     string `yaml:"endpoint"`
	SpanContains string `yaml:"span_contains"`
	Service      string `yaml:"service"`
	Host         string `yaml:"host"`
}

// Onset is the expected start of a regression.
type Onset struct {
	At               string `yaml:"at"`
	ToleranceMinutes int    `yaml:"tolerance_minutes"`
}

// Trajectory is the tool-call DSL (plan §6.4).
type Trajectory struct {
	Required            []CallMatcher   `yaml:"required"`
	AnyOf               [][]CallMatcher `yaml:"any_of"`
	Forbidden           []CallMatcher   `yaml:"forbidden"`
	Order               [][]string      `yaml:"order"`
	MaxCalls            int             `yaml:"max_calls"`
	MaxFailedCalls      int             `yaml:"max_failed_calls"`
	MaxIdenticalRepeats int             `yaml:"max_identical_repeats"`
}

// CallMatcher matches one recorded operation.
type CallMatcher struct {
	Op    string         `yaml:"op"`
	Args  map[string]any `yaml:"args"`
	Write *bool          `yaml:"write"`
	OK    *bool          `yaml:"ok"`
}

// JudgeSpec is the LLM judge's rubric. Expected is shown to the judge only.
type JudgeSpec struct {
	Rubric   []string `yaml:"rubric"`
	Expected string   `yaml:"expected"`
}

// Step is one oracle action, played by the fake model.
type Step struct {
	Shell     string         `yaml:"shell"`
	Stdin     string         `yaml:"stdin"`
	ReadFile  string         `yaml:"read_file"`
	ListFiles string         `yaml:"list_files"`
	Search    map[string]any `yaml:"search"`
	WriteFile *FileStep      `yaml:"write_file"`
	EditFile  *EditStep      `yaml:"edit_file"`
	LoadSkill string         `yaml:"load_skill"`
	AskUser   string         `yaml:"ask_user"`
	Submit    map[string]any `yaml:"submit"`
	Say       string         `yaml:"say"`
}

// FileStep writes a whole file.
type FileStep struct {
	Path    string `yaml:"path"`
	Content string `yaml:"content"`
	// From copies the content of a testdata file instead.
	From string `yaml:"from"`
}

// EditStep replaces one string in a file.
type EditStep struct {
	Path string `yaml:"path"`
	Old  string `yaml:"old"`
	New  string `yaml:"new"`
}

// Duration is a time.Duration that unmarshals from "10m".
type Duration time.Duration

// UnmarshalYAML parses Go duration strings.
func (d *Duration) UnmarshalYAML(n *yaml.Node) error {
	var s string
	if err := n.Decode(&s); err != nil {
		return err
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("duration %q: %w", s, err)
	}
	*d = Duration(v)
	return nil
}

// D returns the time.Duration.
func (d Duration) D() time.Duration { return time.Duration(d) }

var idRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)+$`)

// Validate checks a case for structural errors that YAML decoding cannot.
func (c *Case) Validate() error {
	var errs []string
	add := func(f string, a ...any) { errs = append(errs, fmt.Sprintf(f, a...)) }
	if !idRe.MatchString(c.ID) {
		add("id %q must be kebab-case", c.ID)
	}
	if !slices.Contains(Suites, c.Suite) {
		add("suite %q must be one of %v", c.Suite, Suites)
	} else if !strings.HasPrefix(c.ID, c.Suite+"-") {
		add("id %q must start with %q", c.ID, c.Suite+"-")
	}
	if c.Path != "" && filepath.Base(c.Path) != c.ID+".yaml" {
		add("file %s must be named %s.yaml", c.Path, c.ID)
	}
	if c.Seed == "" {
		add("seed is required")
	}
	if len(c.Arms) == 0 {
		add("arms is required")
	}
	for _, a := range c.Arms {
		if !slices.Contains(AllArms, a) {
			add("unknown arm %q", a)
		}
	}
	switch c.Invoke {
	case "", "slash", "natural":
	default:
		add("invoke must be slash or natural, got %q", c.Invoke)
	}
	if strings.TrimSpace(c.Prompt) == "" {
		add("prompt is required")
	}
	if len(c.Oracle) == 0 {
		add("oracle is required")
	}
	for arm := range c.OracleByArm {
		if !slices.Contains(c.Arms, arm) {
			add("oracle_by_arm names arm %q that the case does not run", arm)
		}
	}
	if c.Expect.Answer == nil {
		add("%s cases need expect.answer", c.Suite)
	}
	if c.Distractor == "" && !slices.Contains(c.Tags, "control") {
		add("distractor is required (or tag the case control)")
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s: %s", c.ID, strings.Join(errs, "; "))
	}
	return nil
}

// HasTag reports whether the case carries tag.
func (c *Case) HasTag(tag string) bool { return slices.Contains(c.Tags, tag) }

// OracleFor returns the oracle steps for an arm.
func (c *Case) OracleFor(arm string) []Step {
	if s, ok := c.OracleByArm[arm]; ok {
		return s
	}
	return c.Oracle
}

// LoadCases reads every cases/<suite>/*.yaml under root. TWEVALS_ONLY (a
// comma-separated list of ids) limits loading to those cases, so work in
// progress on other cases cannot break a run.
func LoadCases(root string) ([]*Case, error) {
	var out []*Case
	seen := map[string]string{}
	only := map[string]bool{}
	for _, id := range strings.Split(os.Getenv("TWEVALS_ONLY"), ",") {
		if id = strings.TrimSpace(id); id != "" {
			only[id] = true
		}
	}
	for _, suite := range Suites {
		files, err := filepath.Glob(filepath.Join(root, "cases", suite, "*.yaml"))
		if err != nil {
			return nil, err
		}
		sort.Strings(files)
		for _, f := range files {
			if len(only) > 0 && !only[strings.TrimSuffix(filepath.Base(f), ".yaml")] {
				continue
			}
			c, err := LoadCase(f)
			if err != nil {
				return nil, err
			}
			if c.Suite != suite {
				return nil, fmt.Errorf("%s: suite %q does not match directory %q", f, c.Suite, suite)
			}
			if prev, dup := seen[c.ID]; dup {
				return nil, fmt.Errorf("duplicate case id %s in %s and %s", c.ID, prev, f)
			}
			seen[c.ID] = f
			out = append(out, c)
		}
	}
	return out, nil
}

// LoadCase reads and validates one case file. Unknown keys are errors.
func LoadCase(path string) (*Case, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	dec := yaml.NewDecoder(strings.NewReader(string(raw)))
	dec.KnownFields(true)
	var c Case
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	c.Path = path
	if c.Invoke == "" {
		c.Invoke = "natural"
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &c, nil
}
