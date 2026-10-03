package accounts

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/BloomyInDev/my-usage/internal/provider"
)

type fakeCreds struct {
	Token string `yaml:"token"`
}

func (c fakeCreds) Marshal() ([]byte, error) { return yaml.Marshal(c) }

type fakeProvider struct{ provider.Provider }

func (fakeProvider) Name() string { return "fake" }
func (fakeProvider) ParseCredentials(raw []byte) (provider.Credentials, error) {
	var c fakeCreds
	if err := yaml.Unmarshal(raw, &c); err != nil {
		return nil, err
	}
	return c, nil
}

func registry() *provider.Registry { return provider.NewRegistry(fakeProvider{}) }

func TestParseEncodeRoundTrip(t *testing.T) {
	raw := []byte("provider: fake\nname: main\ncredentials:\n  token: abc\n")
	a, p, err := Parse(raw, registry())
	if err != nil {
		t.Fatal(err)
	}
	if p.Name() != "fake" || a.Name != "main" || a.Credentials.(fakeCreds).Token != "abc" {
		t.Fatalf("unexpected account %+v", a)
	}
	out, err := Encode(a)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != string(raw) {
		t.Fatalf("round trip changed the file:\n%s", out)
	}
}

func TestParseRejects(t *testing.T) {
	tests := map[string]string{
		"unknown provider": "provider: nope\nname: x\ncredentials:\n  token: a\n",
		"no name":          "provider: fake\ncredentials:\n  token: a\n",
		"no credentials":   "provider: fake\nname: x\n",
		"unknown field":    "provider: fake\nname: x\ncolour: red\ncredentials:\n  token: a\n",
		"not yaml":         "provider: [",
	}
	for name, raw := range tests {
		t.Run(name, func(t *testing.T) {
			if _, _, err := Parse([]byte(raw), registry()); err == nil {
				t.Fatal("want an error")
			}
		})
	}
}

func TestWriteAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.yaml")
	for _, content := range []string{"one", "two"} {
		if err := WriteAtomic(path, []byte(content)); err != nil {
			t.Fatal(err)
		}
		got, _ := os.ReadFile(path)
		if string(got) != content {
			t.Fatalf("got %q, want %q", got, content)
		}
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v, want 0600", info.Mode().Perm())
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("temp file left behind: %v", entries)
	}
}

func TestList(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"b.yaml", "a.yml", ".c.yaml.123.tmp", ".hidden.yaml", "notes.txt", "d.json"} {
		os.WriteFile(filepath.Join(dir, n), nil, 0o600)
	}
	os.Mkdir(filepath.Join(dir, "sub.yaml"), 0o700)

	got, err := Dir(dir).List()
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, p := range got {
		names = append(names, filepath.Base(p))
	}
	if want := []string{"a.yml", "b.yaml"}; !slices.Equal(names, want) {
		t.Fatalf("got %v, want %v", names, want)
	}
	if strings.Contains(strings.Join(got, ""), "sub") {
		t.Fatal("directory listed")
	}
}
