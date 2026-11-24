package app_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"unicode"
)

// readPrefixes name the service methods that only read (the prefix is a
// whole word: Can matches CanEdit, not Cancel). A read is one or a few
// queries, already SQL spans under the request, so it needs no span of its
// own.
var readPrefixes = []string{
	"Get", "List", "Count", "Find", "Feed", "Facets", "Detail", "Details", "Status", "Effective", "Is", "Has",
	"Can", "Stats", "Preferences", "Connections", "Targets", "Unread", "Expiring", "Live", "Deliveries",
}

// unspannedOperations are the writes that deliberately open no span, each
// with its reason. Everything else a module service does opens one with
// tracing.Begin.
var unspannedOperations = map[string]string{
	// Run on every request or audited change: a span each would bury the
	// trace, and their SQL, cache and HTTP spans already show them.
	"activity.Recorder.Record":                 "one INSERT per audited change",
	"activity.Recorder.ImpersonationStarted":   "one INSERT per audited change",
	"activity.Recorder.ImpersonationRequested": "one INSERT per audited change",
	"activity.Recorder.ImpersonationDenied":    "one INSERT per audited change",
	"activity.Recorder.Login":                  "one INSERT per audited change",
	"activity.Recorder.SessionsRevoked":        "one INSERT per audited change",
	"activity.Recorder.TwoFactorChanged":       "one INSERT per audited change",
	"activity.Recorder.InvitationAccepted":     "one INSERT per audited change",
	"activity.Recorder.EmailChanged":           "one INSERT per audited change",
	"activity.Recorder.Maintenance":            "one INSERT per audited change",
	"webhooks.Service.Recorded":                "runs for every activity entry; each delivery it queues is a job with a span",
	"service.Guard.Require":                    "a plan check on every limited write",
	"service.Guard.Limit":                      "a plan check on every limited write",
	"service.Guard.WithinLimit":                "a plan check on every limited write",
	"service.Guard.RequireWithinLimit":         "a plan check on every limited write",
	"service.Guard.QuotaFor":                   "a plan check on every limited write",
	"service.Guard.RequireStorage":             "a plan check on every limited write",
	"authn.OpaqueAuthenticator.Verify":         "runs on every authenticated request",
	"authn.OpaqueAuthenticator.Issue":          "inside the sign-in span that issues the token",
	"authn.OpaqueAuthenticator.IssueSetup":     "inside the sign-in span that issues the token",
	"authn.OTPService.Clear":                   "one cache delete after a sign-in",
	"authn.Throttle.TooMany":                   "a cache read on every sign-in attempt",
	"authn.Throttle.Hit":                       "a cache write on every sign-in attempt",
	"authn.Throttle.Clear":                     "a cache write on every sign-in attempt",
	"backup.HealthCheck.Run":                   "runs on every health probe",
	"dataprocessing.Service.UpdateProgress":    "runs per imported row",
	"oauth.Service.AuthenticateClient":         "a credential check inside Token, which has the span",
	"oauth.Service.VerifyMCP":                  "runs on every MCP request",
	"queue.dispatcherWorker.Work":              "opens the job span (job <kind>) itself, linked to the request that queued it",
	"queue.RiverEngine.Enqueue":                "every queued job; its job span links back to this request",
	"queue.RiverEngine.EnqueueTx":              "every queued job; its job span links back to this request",
	"queue.RiverEngine.Start":                  "worker start-up",
	"queue.RiverEngine.Stop":                   "worker shutdown",
	"queue.RiverEngine.Once":                   "runs the queue once in tests and tools; each job has its span",
	"queue.SyncEngine.Enqueue":                 "runs the job inline; the job's own service spans show it",
	"queue.SyncEngine.EnqueueTx":               "runs the job inline; the job's own service spans show it",
	"queue.HealthCheck.Run":                    "runs on every health probe",
	"maintenance.Checker.Current":              "a cached read on every request",
	// The payment drivers: bKash is HTTP (client spans, under billing.charge);
	// the manual methods do no I/O at all.
	"billing.BkashDriver.Initiate":              "HTTP client spans under billing.charge",
	"billing.BkashDriver.Verify":                "HTTP client spans under billing.charge",
	"billing.BkashDriver.HandleCallback":        "HTTP client spans under billing.gateway_callback",
	"billing.BkashDriver.Refund":                "HTTP client spans",
	"billing.CashDriver.Initiate":               "no I/O",
	"billing.CashDriver.Verify":                 "no I/O",
	"billing.CashDriver.HandleCallback":         "no I/O",
	"billing.CashDriver.Refund":                 "no I/O",
	"billing.BankTransferDriver.Initiate":       "no I/O",
	"billing.BankTransferDriver.Verify":         "no I/O",
	"billing.BankTransferDriver.HandleCallback": "no I/O",
	"billing.BankTransferDriver.Refund":         "no I/O",
	"billing.NagadDriver.Initiate":              "not implemented",
	"billing.NagadDriver.Verify":                "not implemented",
	"billing.NagadDriver.HandleCallback":        "not implemented",
	"billing.NagadDriver.Refund":                "not implemented",
	// Reads whose names do not start with a read prefix.
	"access.RoleService.Grantable":           "a read: the permissions the caller may grant",
	"access.RoleService.Permissions":         "a read: the permissions of a role",
	"access.PermissionService.ForUser":       "a read: the permissions of a user",
	"announcements.Service.TenantTotalsPage": "a read: one page of counts",
	"authn.Service.Account":                  "a read: the caller's account",
	"authn.Service.ConfirmPasswordOnly":      "calls ConfirmPassword, which has the span",
	"authn.Service.TwoFactorRequired":        "a read: the two-factor policy",
	"authn.TwoFactor.Method":                 "a read: the method of a user",
	"authn.Recovery.PeekInvitation":          "a read: checks an invitation link",
	"backup.Service.Config":                  "a read: the effective backup settings",
	"dataprocessing.Service.ActorNames":      "a read: names for a page of jobs",
	"files.Service.URL":                      "builds a URL, no I/O",
	"files.Service.PhotoURL":                 "a read: one avatar URL",
	"files.Service.PhotoURLs":                "a read: avatar URLs for a page",
	"oauth.Clients.Resolve":                  "a read: the registration of a client (cached)",
	"oauth.Service.ResolveClient":            "a read: the registration of a client",
	"oauth.Service.TenantPickable":           "a read for the consent page",
	"oauth.Service.ConsentTenants":           "a read for the consent page",
	"oauth.Service.FirstBranch":              "a read for the consent page",
	"onboarding.Service.Progress":            "a read: the setup checklist",
	"privacy.Service.Deletion":               "a read: the pending deletion",
	"privacy.Service.ErasedSince":            "a read: ids for a backup restore",
	"privacy.Service.Coverage":               "a read: the coverage report",
	"service.InvoiceService.For":             "a read: one invoice",
	"service.UsageService.For":               "a read: the plan usage figures",
	"tenants.Service.Organization":           "a read: the caller's organization",
}

// TestServiceWritesOpenASpan keeps tracing complete: every exported write of
// a module service, of authn, of maintenance mode and of the queue admin
// opens a span with tracing.Begin, or is
// listed above with the reason it does not. Reads, handlers, routes,
// repositories and privacy contributors are not checked; the developer
// dashboard is tooling and is skipped.
func TestServiceWritesOpenASpan(t *testing.T) {
	var missing []string
	for _, root := range []string{"../modules", "../authn", "../platform/maintenance", "../platform/queue"} {
		// The platform packages hide their implementations behind
		// interfaces (the queue admin is riverAdmin), so every type counts.
		allTypes := strings.HasPrefix(root, "../platform/")
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				if entry.Name() == "dev" {
					return filepath.SkipDir
				}
				return nil
			}
			if !checkedFile(entry.Name()) {
				return nil
			}
			missing = append(missing, unspanned(t, path, allTypes)...)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("%d service writes open no span; add `ctx, end := tracing.Begin(ctx, \"<module>.<operation>\", ...)` "+
			"and `defer end(&err)`, or list them in unspannedOperations with the reason:\n  %s",
			len(missing), strings.Join(missing, "\n  "))
	}
}

func checkedFile(name string) bool {
	if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
		return false
	}
	for _, skipped := range []string{"handler", "routes.go", "repository", "request.go", "model.go", "privacy.go"} {
		if strings.Contains(name, skipped) {
			return false
		}
	}
	return true
}

// unspanned lists the methods in one file that should open a span and do not.
func unspanned(t *testing.T, path string, allTypes bool) []string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	var out []string
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv == nil || fn.Body == nil || !fn.Name.IsExported() || !takesContext(fn) {
			continue
		}
		receiver := receiverName(fn)
		if (!allTypes && !ast.IsExported(receiver)) || isPersistence(receiver) || isRead(fn.Name.Name) {
			continue
		}
		key := file.Name.Name + "." + receiver + "." + fn.Name.Name
		if _, listed := unspannedOperations[key]; listed {
			continue
		}
		if !callsBegin(fn.Body) {
			out = append(out, key+"  ("+filepath.ToSlash(path)+")")
		}
	}
	return out
}

// isPersistence reports handlers, repositories and stores: their work shows
// as the request's span and its SQL spans.
func isPersistence(receiver string) bool {
	for _, suffix := range []string{"Handler", "Repository", "Store"} {
		if strings.HasSuffix(receiver, suffix) {
			return true
		}
	}
	return false
}

func takesContext(fn *ast.FuncDecl) bool {
	params := fn.Type.Params.List
	if len(params) == 0 {
		return false
	}
	selector, ok := params[0].Type.(*ast.SelectorExpr)
	return ok && selector.Sel.Name == "Context"
}

func receiverName(fn *ast.FuncDecl) string {
	expr := fn.Recv.List[0].Type
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	if ident, ok := expr.(*ast.Ident); ok {
		return ident.Name
	}
	return ""
}

func isRead(name string) bool {
	for _, prefix := range readPrefixes {
		rest, ok := strings.CutPrefix(name, prefix)
		if ok && (rest == "" || unicode.IsUpper(rune(rest[0]))) {
			return true
		}
	}
	return false
}

func callsBegin(body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return !found
		}
		if selector, ok := call.Fun.(*ast.SelectorExpr); ok && selector.Sel.Name == "Begin" {
			if pkg, ok := selector.X.(*ast.Ident); ok && pkg.Name == "tracing" {
				found = true
			}
		}
		return !found
	})
	return found
}
