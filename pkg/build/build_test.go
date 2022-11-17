// Copyright 2022 Chainguard, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package build

import (
	"io/ioutil"
	"path/filepath"
	"testing"

	apko_types "chainguard.dev/apko/pkg/build/types"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

const defaultTemplateYaml = `package:
  name: nginx
  version: 100
  test: ${{package.name}}
`

const templatized = `package:
  name: {{ .Package }}
  version: {{ .Version }}
  test: ${{package.name}}
`

func TestLoadConfiguration(t *testing.T) {
	expected := &Configuration{
		Package: Package{
			Name:    "nginx",
			Version: "100",
		},
		Subpackages: []Subpackage{},
	}
	expected.Environment.Accounts.Users = []apko_types.User{{
		UserName: "build",
		UID:      1000,
		GID:      1000,
	}}
	expected.Environment.Accounts.Groups = []apko_types.Group{{
		GroupName: "build",
		GID:       1000,
		Members:   []string{"build"},
	}}

	dir := t.TempDir()
	f := filepath.Join(dir, "config")
	if err := ioutil.WriteFile(f, []byte(defaultTemplateYaml), 0755); err != nil {
		t.Fatal(err)
	}

	cfg := &Configuration{}
	if err := cfg.Load(f); err != nil {
		t.Fatal(err)
	}
	if d := cmp.Diff(expected, cfg); d != "" {
		t.Fatalf("actual didn't match expected: %s", d)
	}
}

func TestLoadConfiguration_RangeSubpackages(t *testing.T) {
	contents := `
package:
  name: hello
  version: world

pipeline:
- name: hello
  runs: world

data:
  - name: ninja-turtles
    items:
    - key: Michelangelo
      value: orange
    - key: Raphael
      value: red
    - key: Leonardo
      value: blue
    - key: Donatello
      value: purple
  - name: animals
    items:
    - key: dogs
      value: loyal
    - key: cats
      value: angry
    - key: turtles
      value: slow

subpackages:
  - range: animals
    name: ${{range.key}}
    pipeline:
      - runs: ${{range.key}} are ${{range.value}}
  - range: ninja-turtles
    name: ${{range.key}}
    pipeline:
      - runs: ${{range.key}}'s color is ${{range.value}}
`

	expected := []Subpackage{{
		Name: "dogs",
		Pipeline: []Pipeline{{
			Runs: "dogs are loyal",
		}},
	}, {
		Name: "cats",
		Pipeline: []Pipeline{{
			Runs: "cats are angry",
		}},
	}, {
		Name: "turtles",
		Pipeline: []Pipeline{{
			Runs: "turtles are slow",
		}},
	}, {
		Name: "Michelangelo",
		Pipeline: []Pipeline{{
			Runs: "Michelangelo's color is orange",
		}},
	}, {
		Name: "Raphael",
		Pipeline: []Pipeline{{
			Runs: "Raphael's color is red",
		}},
	}, {
		Name: "Leonardo",
		Pipeline: []Pipeline{{
			Runs: "Leonardo's color is blue",
		}},
	}, {
		Name: "Donatello",
		Pipeline: []Pipeline{{
			Runs: "Donatello's color is purple",
		}},
	}}

	dir := t.TempDir()
	f := filepath.Join(dir, "config")
	if err := ioutil.WriteFile(f, []byte(contents), 0755); err != nil {
		t.Fatal(err)
	}

	cfg := &Configuration{}
	if err := cfg.Load(f); err != nil {
		t.Fatal(err)
	}
	if d := cmp.Diff(expected, cfg.Subpackages, cmpopts.IgnoreUnexported(Pipeline{})); d != "" {
		t.Fatalf("actual didn't match expected: %s", d)
	}
}
