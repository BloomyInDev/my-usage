// Package accounts reads and writes the account files: one YAML file per
// account, holding the provider name, a label-safe account name and the
// provider's own credentials.
package accounts

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/BloomyInDev/my-usage/internal/provider"
)

type Account struct {
	Provider    string
	Name        string
	Credentials provider.Credentials
}

// file is the on-disk shape. Credentials stay a node so the provider decides
// their format.
type file struct {
	Provider    string    `yaml:"provider"`
	Name        string    `yaml:"name"`
	Credentials yaml.Node `yaml:"credentials"`
}

// Parse decodes an account file and hands its credentials to the provider.
// It returns the provider so the caller does not look it up twice.
func Parse(raw []byte, reg *provider.Registry) (Account, provider.Provider, error) {
	var f file
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(&f); err != nil {
		return Account{}, nil, fmt.Errorf("decode account file: %w", err)
	}
	if strings.TrimSpace(f.Name) == "" {
		return Account{}, nil, errors.New("account file has no name")
	}
	if f.Credentials.Kind == 0 {
		return Account{}, nil, errors.New("account file has no credentials")
	}
	p, ok := reg.Get(f.Provider)
	if !ok {
		return Account{}, nil, fmt.Errorf("unknown provider %q", f.Provider)
	}
	credRaw, err := yaml.Marshal(&f.Credentials)
	if err != nil {
		return Account{}, nil, fmt.Errorf("re-encode credentials: %w", err)
	}
	creds, err := p.ParseCredentials(credRaw)
	if err != nil {
		return Account{}, nil, fmt.Errorf("parse %s credentials: %w", f.Provider, err)
	}
	return Account{Provider: f.Provider, Name: f.Name, Credentials: creds}, p, nil
}

// Encode is the inverse of Parse.
func Encode(a Account) ([]byte, error) {
	credRaw, err := a.Credentials.Marshal()
	if err != nil {
		return nil, fmt.Errorf("marshal credentials: %w", err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(credRaw, &doc); err != nil {
		return nil, fmt.Errorf("credentials are not valid YAML: %w", err)
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 {
		return nil, errors.New("credentials must marshal to one YAML document")
	}
	var out bytes.Buffer
	enc := yaml.NewEncoder(&out)
	enc.SetIndent(2)
	if err := enc.Encode(file{Provider: a.Provider, Name: a.Name, Credentials: *doc.Content[0]}); err != nil {
		return nil, fmt.Errorf("encode account file: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("encode account file: %w", err)
	}
	return out.Bytes(), nil
}
