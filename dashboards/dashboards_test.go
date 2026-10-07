package dashboards

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var (
	conditionSelector = regexp.MustCompile(`condition_type(=~|!~|=|!=)\\?"([^"\\]*)\\?"`)
	camelCaseLiteral  = regexp.MustCompile(`"([A-Z][A-Za-z0-9]+)"`)
	regexMeta         = regexp.MustCompile(`[.*+?()\[\]{}^$]`)
)

func TestConditionTypesInDashboardsAndRulesAreEmitted(t *testing.T) {
	repo := filepath.Join("..")
	emitted := goStringLiterals(t, filepath.Join(repo, "pkg"))

	sources, err := filepath.Glob(filepath.Join(repo, "dashboards", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	sources = append(sources,
		filepath.Join(repo, "helm", "node-doctor", "templates", "prometheusrule.yaml"),
		filepath.Join(repo, "deployment", "prometheusrule.yaml"),
	)

	for _, src := range sources {
		data, err := os.ReadFile(src)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range conditionSelector.FindAllStringSubmatch(string(data), -1) {
			op, value := m[1], m[2]
			for _, want := range strings.Split(value, "|") {
				if op == "=~" || op == "!~" {
					if regexMeta.MatchString(want) {
						if !anyLiteralMatches(want, emitted) {
							t.Errorf("%s: condition_type regex %q matches no condition emitted in pkg/", filepath.Base(src), want)
						}
						continue
					}
				}
				if !emitted[want] {
					t.Errorf("%s: condition_type %q is never emitted by code in pkg/", filepath.Base(src), want)
				}
			}
		}
	}
}

func anyLiteralMatches(pattern string, literals map[string]bool) bool {
	re, err := regexp.Compile("^(?:" + pattern + ")$")
	if err != nil {
		return false
	}
	for lit := range literals {
		if re.MatchString(lit) {
			return true
		}
	}
	return false
}

func goStringLiterals(t *testing.T, root string) map[string]bool {
	t.Helper()
	literals := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, m := range camelCaseLiteral.FindAllStringSubmatch(string(data), -1) {
			literals[m[1]] = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(literals) == 0 {
		t.Fatal("no string literals found under pkg/")
	}
	return literals
}

func TestDashboardsAreValidJSON(t *testing.T) {
	files, err := filepath.Glob("*.json")
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(files)
	if len(files) == 0 {
		t.Fatal("no dashboards found")
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if !json.Valid(data) {
			t.Errorf("%s is not valid JSON", f)
		}
	}
}
