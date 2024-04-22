// Copyright 2024 Chainguard, Inc.
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

package sca

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"chainguard.dev/melange/pkg/config"
	"chainguard.dev/melange/pkg/util"
	"github.com/chainguard-dev/clog/slogtest"
	"github.com/chainguard-dev/go-apk/pkg/apk"
	"github.com/chainguard-dev/go-apk/pkg/expandapk"
	"github.com/google/go-cmp/cmp"
	"gopkg.in/ini.v1"
)

type testHandle struct {
	pkg apk.Package
	exp *expandapk.APKExpanded
	cfg *config.Configuration
}

func (th *testHandle) PackageName() string {
	return th.pkg.Name
}

func (th *testHandle) Version() string {
	return th.pkg.Version
}

func (th *testHandle) RelativeNames() []string {
	// TODO: Support subpackages?
	return []string{th.pkg.Origin}
}

func (th *testHandle) FilesystemForRelative(pkgName string) (SCAFS, error) {
	if pkgName != th.PackageName() {
		return nil, fmt.Errorf("TODO: implement FilesystemForRelative, %q != %q", pkgName, th.PackageName())
	}

	return th.exp.TarFS, nil
}

func (th *testHandle) Filesystem() (SCAFS, error) {
	return th.exp.TarFS, nil
}

func (th *testHandle) Options() config.PackageOption {
	return th.cfg.Package.Options
}

func (th *testHandle) BaseDependencies() config.Dependencies {
	return th.cfg.Package.Dependencies
}

// TODO: Loose coupling.
func handleFromApk(ctx context.Context, t *testing.T, apkfile, melangefile string) *testHandle {
	t.Helper()
	file, err := os.Open(filepath.Join("testdata", apkfile))
	if err != nil {
		t.Fatal(err)
	}

	exp, err := expandapk.ExpandApk(ctx, file, "")
	if err != nil {
		t.Fatal(err)
	}

	// Get the package name
	info, err := exp.ControlFS.Open(".PKGINFO")
	if err != nil {
		t.Fatal(err)
	}
	defer info.Close()

	cfg, err := ini.ShadowLoad(info)
	if err != nil {
		t.Fatal(err)
	}

	var pkg apk.Package
	if err = cfg.MapTo(&pkg); err != nil {
		t.Fatal(err)
	}
	pkg.BuildTime = time.Unix(pkg.BuildDate, 0).UTC()
	pkg.InstalledSize = pkg.Size
	pkg.Size = uint64(exp.Size)
	pkg.Checksum = exp.ControlHash

	pkgcfg, err := config.ParseConfiguration(ctx, filepath.Join("testdata", melangefile))
	if err != nil {
		t.Fatal(err)
	}

	return &testHandle{
		pkg: pkg,
		exp: exp,
		cfg: pkgcfg,
	}
}

func TestExecableSharedObjects(t *testing.T) {
	ctx := slogtest.TestContextWithLogger(t)
	th := handleFromApk(ctx, t, "libcap-2.69-r0.apk", "neon.yaml")
	defer th.exp.Close()

	got := config.Dependencies{}
	if err := Analyze(ctx, th, &got); err != nil {
		t.Fatal(err)
	}

	want := config.Dependencies{
		Runtime: []string{
			"so:ld-linux-aarch64.so.1",
			"so:libc.so.6",
			"so:libcap.so.2",
			"so:libpsx.so.2",
		},
		Provides: []string{
			"so:libcap.so.2=2",
			"so:libpsx.so.2=2",
		},
	}

	got.Runtime = util.Dedup(got.Runtime)
	got.Provides = util.Dedup(got.Provides)

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Analyze(): (-want, +got):\n%s", diff)
	}
}

// test a fips like go binary package for SCA depends
// Chainguard go-fips toolchain generates binaries like these
// which at runtime require openssl and fips provider
func TestGoFipsBinDeps(t *testing.T) {
	ctx := slogtest.TestContextWithLogger(t)

	var ldso, archdir string
	switch runtime.GOARCH {
	case "arm64":
		ldso = "so:ld-linux-aarch64.so.1"
		archdir = "aarch64"
	case "amd64":
		ldso = "so:ld-linux-x86-64.so.2"
		archdir = "x86_64"
	}

	th := handleFromApk(ctx, t, fmt.Sprintf("go-fips-bin/packages/%s/go-fips-bin-v0.0.1-r0.apk", archdir), "go-fips-bin/go-fips-bin.yaml")
	defer th.exp.Close()

	got := config.Dependencies{}
	if err := Analyze(ctx, th, &got); err != nil {
		t.Fatal(err)
	}

	want := config.Dependencies{
		Runtime: []string{
			"openssl-config-fipshardened",
			ldso,
			"so:libc.so.6",
			"so:libcrypto.so.3",
			"so:libssl.so.3",
		},
		Provides: []string{
			"cmd:go-fips-bin=v0.0.1-r0",
		},
	}

	got.Runtime = util.Dedup(got.Runtime)
	got.Provides = util.Dedup(got.Provides)

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Analyze(): (-want, +got):\n%s", diff)
	}
}

func TestVendoredPkgConfig(t *testing.T) {
	ctx := slogtest.TestContextWithLogger(t)
	// Generated by:
	// curl -L https://packages.wolfi.dev/os/aarch64/neon-4604-r0.apk > neon.apk
	// tardegrade <neon.apk echo $(tar -tf neon.apk| head -n 2) $(tar -tf neon.apk | grep pkgconfig) usr/libexec/neon/v14/lib/libecpg_compat.so.3.14 > neon-4604-r0.apk
	th := handleFromApk(ctx, t, "neon-4604-r0.apk", "neon.yaml")
	defer th.exp.Close()

	got := config.Dependencies{}
	if err := Analyze(ctx, th, &got); err != nil {
		t.Fatal(err)
	}

	want := config.Dependencies{
		Runtime: []string{
			// We only include libecpg_compat.so.3 to test that "libexec" isn't treated as a library directory.
			// These are dependencies of libecpg_compat.so.3, but if we had the whole neon APK it would look different.
			"so:libecpg.so.6", "so:libpgtypes.so.3", "so:libpq.so.5", "so:libc.so.6", "so:ld-linux-aarch64.so.1",
		},
		Vendored: []string{
			"so:libecpg_compat.so.3=3",
			"pc:libecpg=14.10",
			"pc:libecpg_compat=14.10",
			"pc:libpgtypes=14.10",
			"pc:libpq=14.10",
			"pc:libecpg=15.5",
			"pc:libecpg_compat=15.5",
			"pc:libpgtypes=15.5",
			"pc:libpq=15.5",
		},
	}

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Analyze(): (-want, +got):\n%s", diff)
	}
}

func TestUnstableSonames(t *testing.T) {
	ctx := slogtest.TestContextWithLogger(t)
	// Generated by:
	// curl -L https://packages.wolfi.dev/os/aarch64/aws-c-s3-0.4.9-r0.apk > aws.apk
	// tardegrade <aws.apk echo $(tar -tf aws.apk| head -n 6) > aws-c-s3-0.4.9-r0.apk
	th := handleFromApk(ctx, t, "aws-c-s3-0.4.9-r0.apk", "aws-c-s3.yaml")
	defer th.exp.Close()

	got := config.Dependencies{}
	if err := Analyze(ctx, th, &got); err != nil {
		t.Fatal(err)
	}

	want := config.Dependencies{
		Runtime: []string{
			"so:libaws-c-s3.so.0unstable",
			"so:libaws-c-auth.so.1.0.0",
			"so:libaws-checksums.so.1.0.0",
			"so:libaws-c-http.so.1.0.0",
			"so:libaws-c-io.so.1.0.0",
			"so:libaws-c-cal.so.1.0.0",
			"so:libaws-c-common.so.1",
			"so:libc.so.6",
			"so:ld-linux-aarch64.so.1",
		},
		Provides: []string{"so:libaws-c-s3.so.0unstable=0"},
	}

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Analyze(): (-want, +got):\n%s", diff)
	}
}

func TestShbangDeps(t *testing.T) {
	ctx := slogtest.TestContextWithLogger(t)
	th := handleFromApk(ctx, t, "shbang-test-1-r1.apk", "shbang-test.yaml")
	defer th.exp.Close()

	want := config.Dependencies{
		Runtime: util.Dedup([]string{
			"cmd:bash",
			"cmd:envDashSCmd",
			"cmd:python3.12",
		}),
		Provides: nil,
	}

	got := config.Dependencies{}
	if err := generateShbangDeps(ctx, th, &got); err != nil {
		t.Fatal(err)
	}

	got.Runtime = util.Dedup(got.Runtime)
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Analyze(): (-want, +got):\n%s", diff)
	}
}
