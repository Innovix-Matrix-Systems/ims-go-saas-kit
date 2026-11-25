package rename

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const starterModule = "github.com/Innovix-Matrix-Systems/ims-go-saas-kit"

func names(t *testing.T, opts Options) Names {
	t.Helper()
	n, err := Resolve(opts)
	if err != nil {
		t.Fatalf("Resolve(%+v) error = %v", opts, err)
	}
	return n
}

func TestResolveDerivesNames(t *testing.T) {
	n := names(t, Options{Module: "github.com/acme/shop-api"})
	if n.Name != "shop-api" || n.Binary != "shop-api" || n.Title != "Shop API" || n.EnvPrefix != "SHOP_API_" {
		t.Fatalf("derived names = %+v", n)
	}
	n = names(t, Options{Module: "gitlab.com/acme/platform/shop-api", Binary: "shop", Title: "Acme Shop"})
	if n.Name != "shop-api" || n.Binary != "shop" || n.Title != "Acme Shop" || n.EnvPrefix != "SHOP_" {
		t.Fatalf("explicit names = %+v", n)
	}
}

func TestResolveRejectsBadNames(t *testing.T) {
	for _, opts := range []Options{
		{},
		{Module: "shop-api"},
		{Module: "github.com/acme/shop api"},
		{Module: "github.com/acme/shop-api", Name: "Shop_API"},
		{Module: "github.com/acme/shop-api", Binary: "shop.exe"},
	} {
		if _, err := Resolve(opts); err == nil {
			t.Errorf("Resolve(%+v) succeeded, want an error", opts)
		}
	}
}

func TestRewrite(t *testing.T) {
	n := names(t, Options{Module: "github.com/acme/shop-api", Binary: "shop", Title: "Acme Shop"})
	cases := []struct{ name, in, want string }{
		{"module import", `import "github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/cli"`,
			`import "github.com/acme/shop-api/internal/cli"`},
		{"makefile", "APP := ims\nMODULE := github.com/Innovix-Matrix-Systems/ims-go-saas-kit",
			"APP := shop\nMODULE := github.com/acme/shop-api"},
		{"titles", "# IMS Go SaaS Kit\n<title>Dev Dashboard — IMS</title> IMS Go SaaS docs",
			"# Acme Shop\n<title>Dev Dashboard — Acme Shop</title> Acme Shop docs"},
		{"deploy paths", "BASE_DIR=\"${IMS_BASE_DIR:-/opt/ims/ims-go-saas-kit}\"",
			"BASE_DIR=\"${SHOP_BASE_DIR:-/opt/shop/shop-api}\""},
		{"commands", "run `ims migrate && ims permission:sync`; CACHE_PREFIX=ims-",
			"run `shop migrate && shop permission:sync`; CACHE_PREFIX=shop-"},
		{"repo and site", `"name": "ims-go-saas-kit"`, `"name": "shop-api"`},
		{"seeded e-mails stay", `{"email":"superadmin@ims.com"} developer@ims.com`,
			`{"email":"superadmin@ims.com"} developer@ims.com`},
		{"credits stay", "[NestJS](https://github.com/Innovix-Matrix-Systems/ims-nest-api-starter) · https://ims-go-saas-kit.innovixmatrixsystem.workers.dev",
			"[NestJS](https://github.com/Innovix-Matrix-Systems/ims-nest-api-starter) · https://ims-go-saas-kit.innovixmatrixsystem.workers.dev"},
		{"words containing ims stay", "claims, imsi, IMSI, aims", "claims, imsi, IMSI, aims"},
		{"keep markers", "ims serve\n<!-- rename:keep -->\nmake rename turns `ims` into yours\n<!-- rename:end -->\nims migrate",
			"shop serve\n<!-- rename:keep -->\nmake rename turns `ims` into yours\n<!-- rename:end -->\nshop migrate"},
		{"partial seeded e-mail stays", "/dev/api/actions?search=superadmin@ims", "/dev/api/actions?search=superadmin@ims"},
		{"url-encoded titles", "otpauth://totp/IMS%20Go%20SaaS:jane@acme.test?issuer=IMS+Go+SaaS&period=30 IMS%20Go%20SaaS%20Kit",
			"otpauth://totp/Acme%20Shop:jane@acme.test?issuer=Acme+Shop&period=30 Acme%20Shop"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, _ := Rewrite(tc.in, starterModule, n)
			if got != tc.want {
				t.Fatalf("Rewrite()\n got: %s\nwant: %s", got, tc.want)
			}
		})
	}
}

// A new value that itself contains an old name is never rewritten again.
func TestRewriteDoesNotRewriteNewValues(t *testing.T) {
	n := names(t, Options{Module: "github.com/ims/ims-go-saas-kit-fork", Name: "ims-portal", Binary: "ims"})
	got, _ := Rewrite("module github.com/Innovix-Matrix-Systems/ims-go-saas-kit\nAPP := ims # IMS Go SaaS Kit", starterModule, n)
	want := "module github.com/ims/ims-go-saas-kit-fork\nAPP := ims # IMS Portal"
	if got != want {
		t.Fatalf("Rewrite()\n got: %s\nwant: %s", got, want)
	}
}

// A whole run on a small project tree (not a git checkout).
func TestRunRenamesAProject(t *testing.T) {
	root := t.TempDir()
	write := func(rel, content string) {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module "+starterModule+"\n\ngo 1.26\n")
	write("cmd/app/main.go", "package main\n\nimport \"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/cli\"\n\nfunc main() { cli.Run() }\n")
	write("cmd/rename/main.go", "package main\n\n// renames ims-go-saas-kit\nimport _ \"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/rename\"\n")
	write("internal/rename/rename.go", "package rename\n\nconst OldBinary = \"ims\"\n")
	write("deploy/systemd/ims-go-saas-kit-worker.service", "ExecStart=/opt/ims/ims-go-saas-kit/current/ims queue:work\n")
	write("node_modules/x/index.js", "ims")

	dry, err := Run(root, Options{Module: "github.com/acme/shop-api", DryRun: true})
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if len(dry.Files) == 0 {
		t.Fatal("dry run found nothing to change")
	}
	if data, _ := os.ReadFile(filepath.Join(root, "go.mod")); !strings.Contains(string(data), starterModule) {
		t.Fatal("dry run changed go.mod")
	}

	if _, err := Run(root, Options{Module: "github.com/acme/shop-api", Binary: "shop"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	read := func(rel string) string {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		return string(data)
	}
	if got := read("go.mod"); !strings.HasPrefix(got, "module github.com/acme/shop-api\n") {
		t.Errorf("go.mod = %q", got)
	}
	if got := read("cmd/app/main.go"); !strings.Contains(got, `"github.com/acme/shop-api/internal/cli"`) {
		t.Errorf("main.go = %q", got)
	}
	if got := read("cmd/rename/main.go"); !strings.Contains(got, "github.com/acme/shop-api/internal/rename") || !strings.Contains(got, "renames ims-go-saas-kit") {
		t.Errorf("cmd/rename gets only the module path: %q", got)
	}
	if got := read("internal/rename/rename.go"); !strings.Contains(got, `"ims"`) {
		t.Errorf("internal/rename was rewritten: %q", got)
	}
	if got := read("deploy/systemd/shop-api-worker.service"); got != "ExecStart=/opt/shop/shop-api/current/shop queue:work\n" {
		t.Errorf("unit = %q", got)
	}
	if _, err := os.Stat(filepath.Join(root, "deploy/systemd/ims-go-saas-kit-worker.service")); !os.IsNotExist(err) {
		t.Error("the old unit file is still there")
	}
	if got := read("node_modules/x/index.js"); got != "ims" {
		t.Errorf("node_modules was rewritten: %q", got)
	}

	// Running again is refused: the starter's module is gone.
	if _, err := Run(root, Options{Module: "github.com/acme/other"}); err == nil {
		t.Fatal("a second rename succeeded, want an error")
	}
}
