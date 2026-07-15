package tui

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

type asyncSourceLogicalPath struct {
	label      string
	kind       string
	completion string
}

// asyncSourceLogicalPaths is deliberately the issue's logical inventory rather
// than a scan for every tea.Msg in this large package. Initial and steady
// refresh are two lifetimes but share one completion struct.
var asyncSourceLogicalPaths = []asyncSourceLogicalPath{
	{label: "initial rebuild", kind: "asyncInitialRebuild", completion: "projectMailRefreshMsg"},
	{label: "steady refresh", kind: "asyncSteadyRefresh", completion: "projectMailRefreshMsg"},
	{label: "session persist", kind: "asyncSessionPersist", completion: "mailPersistMsg"},
	{label: "older-page history", kind: "asyncOlderPage", completion: "mailOlderPageMsg"},
	{label: "exact history count", kind: "asyncExactHistoryCount", completion: "mailHistoryCountMsg"},
	{label: "refresh tick", kind: "asyncRefreshTick", completion: "projectMailTickMsg"},
	{label: "liveness pulse", kind: "asyncLivenessPulse", completion: "pulseTickMsg"},
	{label: "home telemetry", kind: "asyncHomeTelemetry", completion: "homeTelemetryMsg"},
	{label: "external editor request", kind: "asyncEditorRequest", completion: "OpenEditorMsg"},
	{label: "external editor completion", kind: "asyncEditorDone", completion: "EditorDoneMsg"},
	{label: "cold ordinary thread load", kind: "asyncColdThreadLoad", completion: "threadLoadResultMsg"},
	{label: "bound mail send", kind: "asyncBoundSend", completion: "boundSendRequestMsg"},
}

var asyncSourceCompletionProducers = map[string][]string{
	"projectMailRefreshMsg": {"ProjectMailStore.beginRefresh"},
	"mailPersistMsg":        {"MailModel.Update"},
	"mailOlderPageMsg":      {"MailModel.olderPageCmd"},
	"mailHistoryCountMsg":   {"MailModel.historyCountCmd"},
	"projectMailTickMsg":    {"projectMailTickEvery"},
	"pulseTickMsg":          {"pulseTick"},
	"homeTelemetryMsg":      {"MailModel.maybeScheduleHomeTelemetry"},
	"OpenEditorMsg":         {"MailModel.requestEditor"},
	"EditorDoneMsg":         {"MailModel.launchEditor"},
	"threadLoadResultMsg":   {"ThreadLoadCoordinator.request"},
	"boundSendRequestMsg":   {"MailModel.Update"},
}

type asyncSourceConsumerContract struct {
	message string
	owner   string
}

var asyncSourceConsumers = []asyncSourceConsumerContract{
	{message: "projectMailRefreshMsg", owner: "App.Update"},
	{message: "projectMailTickMsg", owner: "App.Update"},
	{message: "mailPersistMsg", owner: "MailModel.Update"},
	{message: "mailOlderPageMsg", owner: "MailModel.Update"},
	{message: "mailHistoryCountMsg", owner: "MailModel.Update"},
	{message: "pulseTickMsg", owner: "MailModel.Update"},
	{message: "homeTelemetryMsg", owner: "MailModel.Update"},
	{message: "OpenEditorMsg", owner: "MailModel.Update"},
	{message: "EditorDoneMsg", owner: "MailModel.Update"},
	{message: "threadLoadResultMsg", owner: "App.Update"},
	{message: "boundSendRequestMsg", owner: "MailModel.Update"},
}

type asyncSourceExclusion struct {
	message string
	reason  string
}

// This is the one real asynchronous message explicitly outside the target-mail
// protocol. Keeping its name and reason here prevents "every completion" from
// becoming an unbounded whole-package promise.
var asyncSourceExclusions = []asyncSourceExclusion{
	{message: "autoRefreshTickMsg", reason: "unrelated app-level non-mail refresh loop"},
}

type asyncSourceInventory struct {
	fset  *token.FileSet
	files map[string]*ast.File
	types map[string]*ast.TypeSpec
	funcs []*ast.FuncDecl
}

func loadAsyncSourceInventory(t *testing.T) *asyncSourceInventory {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read production TUI source directory: %v", err)
	}

	inv := &asyncSourceInventory{
		fset:  token.NewFileSet(),
		files: make(map[string]*ast.File),
		types: make(map[string]*ast.TypeSpec),
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(inv.fset, name, nil, parser.ParseComments)
		if err != nil {
			t.Fatalf("parse production source %s: %v", name, err)
		}
		inv.files[name] = file
		for _, decl := range file.Decls {
			switch decl := decl.(type) {
			case *ast.GenDecl:
				if decl.Tok != token.TYPE {
					continue
				}
				for _, spec := range decl.Specs {
					if typeSpec, ok := spec.(*ast.TypeSpec); ok {
						inv.types[typeSpec.Name.Name] = typeSpec
					}
				}
			case *ast.FuncDecl:
				inv.funcs = append(inv.funcs, decl)
			}
		}
	}
	return inv
}

func TestAsyncEnvelopeSourceInventoryMatchesNamedLogicalKinds(t *testing.T) {
	inv := loadAsyncSourceInventory(t)
	if _, ok := inv.files["async_envelope.go"]; !ok {
		t.Fatalf("async_envelope.go missing: want one shared protocol defining the named target-mail logical kinds")
	}
	if _, ok := inv.types["asyncKind"]; !ok {
		t.Fatalf("async_envelope.go: asyncKind type missing")
	}

	want := make([]string, 0, len(asyncSourceLogicalPaths))
	for _, path := range asyncSourceLogicalPaths {
		want = append(want, path.kind)
	}
	got := inv.constantsOfType("asyncKind")
	if missing, unexpected := setDifference(want, got), setDifference(got, want); len(missing) != 0 || len(unexpected) != 0 {
		t.Fatalf("asyncKind inventory mismatch: got %v; missing %v; unexpected %v; want exactly the named logical paths", got, missing, unexpected)
	}
}

func TestAsyncEnvelopeSourceInventoryEveryCompletionCarriesEnvelope(t *testing.T) {
	inv := loadAsyncSourceInventory(t)
	for _, name := range asyncCompletionNames() {
		assertExactNamedField(t, inv, name, "envelope", "asyncEnvelope")
	}

	assertExactNamedField(t, inv, "OpenEditorMsg", "Text", "string")
	assertExactNamedField(t, inv, "EditorDoneMsg", "Text", "string")
	assertExactNamedField(t, inv, "homeTelemetryMsg", "t", "homeTelemetry")
	if got := inv.namedFieldTypes("EditorDoneMsg", "Generation"); len(got) != 0 {
		t.Errorf("EditorDoneMsg.Generation is a forbidden generation-only identity; target/address/generation must be carried only by envelope asyncEnvelope")
	}
	if got := inv.namedFieldTypes("EditorDoneMsg", "generation"); len(got) != 0 {
		t.Errorf("EditorDoneMsg.generation is a forbidden generation-only identity; target/address/generation must be carried only by envelope asyncEnvelope")
	}
}

func TestAsyncEnvelopeSourceInventoryEveryProducerCapturesEnvelope(t *testing.T) {
	inv := loadAsyncSourceInventory(t)
	var issues []string
	for _, completion := range asyncCompletionNames() {
		issues = append(issues, inv.producerIssues(completion, asyncSourceCompletionProducers[completion])...)
	}
	issues = append(issues, inv.refreshKindCaptureIssues("ProjectMailStore.beginRefresh")...)
	if len(issues) != 0 {
		sort.Strings(issues)
		t.Fatalf("async completion producer inventory is not envelope-complete:\n  - %s", strings.Join(issues, "\n  - "))
	}
}

func TestAsyncEnvelopeSourceInventoryEveryConsumerCallsSharedPredicate(t *testing.T) {
	inv := loadAsyncSourceInventory(t)
	var issues []string
	for _, contract := range asyncSourceConsumers {
		issues = append(issues, inv.consumerIssues(contract.owner, contract.message)...)
	}
	if len(issues) != 0 {
		sort.Strings(issues)
		t.Fatalf("target-mail consumer inventory does not route every completion through one rejecting acceptAsync guard before visible mutation:\n  - %s", strings.Join(issues, "\n  - "))
	}
}

func TestAsyncEnvelopeSourceInventoryRefreshSettlementIsNonPublishing(t *testing.T) {
	inv := loadAsyncSourceInventory(t)
	issues := append(inv.refreshSettlementIssues(), inv.refreshSettlementOrderingIssues()...)
	if len(issues) != 0 {
		sort.Strings(issues)
		t.Fatalf("refresh settlement escaped its exact-token, non-publishing boundary:\n  - %s", strings.Join(issues, "\n  - "))
	}
}

func TestAsyncEnvelopeSourceInventoryForbidsLegacyIdentityPolicies(t *testing.T) {
	inv := loadAsyncSourceInventory(t)
	var surviving []string

	for _, fn := range inv.funcs {
		switch fn.Name.Name {
		case "acceptRefresh", "acceptsTick":
			surviving = append(surviving, "symbol "+functionOwner(fn))
		}
	}
	if _, ok := inv.types["projectMailRuntimeGate"]; ok {
		surviving = append(surviving, "type projectMailRuntimeGate")
	}

	// Payload/cache/lifecycle fields are intentionally not banned here. This list
	// is only the old message-local identity vocabulary that must move into the
	// shared envelope.
	identityFields := []string{"generation", "storeID", "projectID", "activation", "sourceVersion", "chain"}
	types := append(asyncCompletionNames(), "mailRefreshMsg", "projectMailRefreshRequestMsg")
	for _, typeName := range uniqueStrings(types) {
		for _, fieldName := range identityFields {
			if got := inv.namedFieldTypes(typeName, fieldName); len(got) != 0 {
				surviving = append(surviving, "field "+typeName+"."+fieldName)
			}
		}
	}
	if got := inv.namedFieldTypes("EditorDoneMsg", "Generation"); len(got) != 0 {
		surviving = append(surviving, "field EditorDoneMsg.Generation")
	}

	if len(surviving) != 0 {
		surviving = uniqueStrings(surviving)
		t.Fatalf("legacy async identity policies remain (payload fields and lifecycle booleans are not part of this ban): %s", strings.Join(surviving, ", "))
	}
}

func TestAsyncEnvelopeSourceInventoryScopesNonMilestoneAsyncMessagesExplicitly(t *testing.T) {
	inv := loadAsyncSourceInventory(t)
	var issues []string

	completionSet := make(map[string]bool)
	for _, completion := range asyncCompletionNames() {
		completionSet[completion] = true
	}
	for _, exclusion := range asyncSourceExclusions {
		if exclusion.reason == "" {
			issues = append(issues, exclusion.message+": exclusion reason missing")
		}
		if _, ok := inv.types[exclusion.message]; !ok {
			issues = append(issues, exclusion.message+": documented non-milestone async type missing; update the explicit scope inventory if it was intentionally renamed or removed")
		}
		if completionSet[exclusion.message] {
			issues = append(issues, exclusion.message+": explicit exclusion was accidentally added to the named target-mail completion structs")
		}
		if got := inv.namedFieldTypes(exclusion.message, "envelope"); len(got) != 0 {
			issues = append(issues, exclusion.message+": explicit exclusion unexpectedly carries the target-mail envelope")
		}
	}

	// The refresh request is coordination, not another logical path. It must
	// still carry/capture an initial-or-steady envelope and be accepted before work starts.
	if completionSet["projectMailRefreshRequestMsg"] {
		issues = append(issues, "projectMailRefreshRequestMsg: request must not become an additional completion kind")
	}
	issues = append(issues, exactNamedFieldIssues(inv, "projectMailRefreshRequestMsg", "envelope", "asyncEnvelope")...)
	issues = append(issues, inv.producerIssues("projectMailRefreshRequestMsg", []string{"MailModel.requestMailRefresh"})...)
	issues = append(issues, inv.refreshKindCaptureIssues("MailModel.requestMailRefresh")...)
	issues = append(issues, inv.consumerIssues("App.Update", "projectMailRefreshRequestMsg")...)

	// The delayed human-location write is a post-accept side effect, not an
	// additional message kind. It captures the accepted refresh envelope and re-runs the same
	// predicate immediately before the side effect.
	location := inv.findFunction("ProjectMailStore.locationUpdateCmd")
	if location == nil {
		issues = append(issues, "ProjectMailStore.locationUpdateCmd: post-accept human-location side-effect fence missing")
	} else {
		if got := countFieldsOfType(location.Type.Params, "asyncEnvelope"); got != 1 {
			issues = append(issues, fmt.Sprintf("ProjectMailStore.locationUpdateCmd: got %d asyncEnvelope parameters, want exactly 1 captured accepted refresh envelope", got))
		}
		if got := countCalls(location.Body, "acceptAsync"); got != 1 {
			issues = append(issues, fmt.Sprintf("ProjectMailStore.locationUpdateCmd: got %d acceptAsync calls, want exactly 1 immediately before the delayed side effect", got))
		}
	}

	if len(issues) != 0 {
		sort.Strings(issues)
		t.Fatalf("non-milestone/coordination async scope is incomplete:\n  - %s", strings.Join(issues, "\n  - "))
	}
}

func asyncCompletionNames() []string {
	var names []string
	seen := make(map[string]bool)
	for _, path := range asyncSourceLogicalPaths {
		if !seen[path.completion] {
			seen[path.completion] = true
			names = append(names, path.completion)
		}
	}
	return names
}

func (inv *asyncSourceInventory) constantsOfType(typeName string) []string {
	var names []string
	fileNames := sortedFileNames(inv.files)
	for _, fileName := range fileNames {
		file := inv.files[fileName]
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			currentType := ""
			for _, spec := range gen.Specs {
				value, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				if value.Type != nil {
					currentType = expressionName(value.Type)
				}
				if currentType != typeName {
					continue
				}
				for _, name := range value.Names {
					names = append(names, name.Name)
				}
			}
		}
	}
	return uniqueStrings(names)
}

func assertExactNamedField(t *testing.T, inv *asyncSourceInventory, typeName, fieldName, wantType string) {
	t.Helper()
	for _, issue := range exactNamedFieldIssues(inv, typeName, fieldName, wantType) {
		t.Error(issue)
	}
}

func exactNamedFieldIssues(inv *asyncSourceInventory, typeName, fieldName, wantType string) []string {
	if _, ok := inv.types[typeName]; !ok {
		return []string{typeName + ": production type missing"}
	}
	got := inv.namedFieldTypes(typeName, fieldName)
	if len(got) != 1 || got[0] != wantType {
		return []string{fmt.Sprintf("%s: field %s must appear exactly once with type %s; got %v", typeName, fieldName, wantType, got)}
	}
	return nil
}

func (inv *asyncSourceInventory) namedFieldTypes(typeName, fieldName string) []string {
	typeSpec := inv.types[typeName]
	if typeSpec == nil {
		return nil
	}
	structure, ok := typeSpec.Type.(*ast.StructType)
	if !ok {
		return nil
	}
	var types []string
	for _, field := range structure.Fields.List {
		for _, name := range field.Names {
			if name.Name == fieldName {
				types = append(types, expressionName(field.Type))
			}
		}
	}
	return types
}

type asyncLiteralSite struct {
	file      string
	owner     string
	literal   *ast.CompositeLit
	safeNames map[string]bool
	position  token.Position
}

func (inv *asyncSourceInventory) literalSites(typeName string) []asyncLiteralSite {
	var sites []asyncLiteralSite
	for _, fileName := range sortedFileNames(inv.files) {
		file := inv.files[fileName]
		seen := make(map[*ast.CompositeLit]bool)
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			safe := captureDerivedNames(fn.Body)
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				literal, ok := node.(*ast.CompositeLit)
				if !ok || expressionName(literal.Type) != typeName {
					return true
				}
				seen[literal] = true
				sites = append(sites, asyncLiteralSite{
					file:      fileName,
					owner:     functionOwner(fn),
					literal:   literal,
					safeNames: safe,
					position:  inv.fset.Position(literal.Pos()),
				})
				return true
			})
		}
		ast.Inspect(file, func(node ast.Node) bool {
			literal, ok := node.(*ast.CompositeLit)
			if !ok || seen[literal] || expressionName(literal.Type) != typeName {
				return true
			}
			sites = append(sites, asyncLiteralSite{
				file:      fileName,
				owner:     "<package>",
				literal:   literal,
				safeNames: map[string]bool{},
				position:  inv.fset.Position(literal.Pos()),
			})
			return true
		})
	}
	sort.Slice(sites, func(i, j int) bool {
		if sites[i].file != sites[j].file {
			return sites[i].file < sites[j].file
		}
		return sites[i].position.Offset < sites[j].position.Offset
	})
	return sites
}

func (inv *asyncSourceInventory) producerIssues(typeName string, wantOwners []string) []string {
	sites := inv.literalSites(typeName)
	if len(sites) == 0 {
		return []string{typeName + ": no production composite-literal producer found"}
	}

	var issues []string
	var gotOwners []string
	for _, site := range sites {
		gotOwners = append(gotOwners, site.owner)
		location := fmt.Sprintf("%s:%d (%s)", filepath.Base(site.position.Filename), site.position.Line, site.owner)
		var envelopeValues []ast.Expr
		unkeyed := false
		for _, element := range site.literal.Elts {
			keyed, ok := element.(*ast.KeyValueExpr)
			if !ok {
				unkeyed = true
				continue
			}
			key, ok := keyed.Key.(*ast.Ident)
			if ok && key.Name == "envelope" {
				envelopeValues = append(envelopeValues, keyed.Value)
			}
		}
		if unkeyed {
			issues = append(issues, typeName+" producer "+location+": completion literals must be keyed so envelope capture cannot be positional or implicit")
		}
		if len(envelopeValues) != 1 {
			issues = append(issues, fmt.Sprintf("%s producer %s: got %d keyed envelope fields, want exactly 1 captureAsync-derived envelope", typeName, location, len(envelopeValues)))
			continue
		}
		if !expressionDerivedFromCapture(envelopeValues[0], site.safeNames) {
			issues = append(issues, typeName+" producer "+location+": envelope is not initialized through captureAsync (zero/manual envelopes are forbidden)")
		}
	}

	gotOwners = uniqueStrings(gotOwners)
	wantOwners = uniqueStrings(wantOwners)
	if missing, unexpected := setDifference(wantOwners, gotOwners), setDifference(gotOwners, wantOwners); len(missing) != 0 || len(unexpected) != 0 {
		issues = append(issues, fmt.Sprintf("%s producer symbols: got %v; missing %v; unexpected %v", typeName, gotOwners, missing, unexpected))
	}
	return issues
}

func captureDerivedNames(body *ast.BlockStmt) map[string]bool {
	safe := make(map[string]bool)
	changed := true
	for changed {
		changed = false
		ast.Inspect(body, func(node ast.Node) bool {
			switch node := node.(type) {
			case *ast.AssignStmt:
				if len(node.Lhs) != len(node.Rhs) {
					return true
				}
				for i, lhs := range node.Lhs {
					name, ok := lhs.(*ast.Ident)
					if ok && !safe[name.Name] && expressionDerivedFromCapture(node.Rhs[i], safe) {
						safe[name.Name] = true
						changed = true
					}
				}
			case *ast.ValueSpec:
				if len(node.Names) != len(node.Values) {
					return true
				}
				for i, name := range node.Names {
					if !safe[name.Name] && expressionDerivedFromCapture(node.Values[i], safe) {
						safe[name.Name] = true
						changed = true
					}
				}
			}
			return true
		})
	}
	return safe
}

func expressionDerivedFromCapture(expr ast.Expr, safeNames map[string]bool) bool {
	derived := false
	ast.Inspect(expr, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.CallExpr:
			if calledName(node.Fun) == "captureAsync" {
				derived = true
				return false
			}
		case *ast.Ident:
			if safeNames[node.Name] {
				derived = true
				return false
			}
		}
		return !derived
	})
	return derived
}

func (inv *asyncSourceInventory) refreshKindCaptureIssues(owner string) []string {
	fn := inv.findFunction(owner)
	if fn == nil {
		return []string{owner + ": expected initial/steady producer symbol missing"}
	}
	if countCalls(fn.Body, "captureAsync") == 0 {
		return []string{owner + ": missing captureAsync for the shared initial/steady envelope"}
	}
	if usesIdentifier(fn, "asyncInitialRebuild") && usesIdentifier(fn, "asyncSteadyRefresh") {
		return nil
	}

	// A small asyncKind-returning selector helper is allowed, but it must itself
	// name both fixed refresh kinds. Do not chase captureAsync, whose global table
	// necessarily mentions all kinds and would make this producer check vacuous.
	for _, callName := range calledNames(fn.Body) {
		if callName == "captureAsync" {
			continue
		}
		for _, helper := range inv.findFunctionsBySimpleName(callName) {
			if functionReturnsType(helper, "asyncKind") && usesIdentifier(helper, "asyncInitialRebuild") && usesIdentifier(helper, "asyncSteadyRefresh") {
				return nil
			}
		}
	}
	return []string{owner + ": capture must select both asyncInitialRebuild and asyncSteadyRefresh; their shared completion/request is not an additional kind"}
}

func (inv *asyncSourceInventory) consumerIssues(owner, message string) []string {
	fn := inv.findFunction(owner)
	if fn == nil {
		return []string{owner + ": consumer function missing for " + message}
	}
	clauses := typeSwitchClauses(fn, message)
	if len(clauses) != 1 {
		return []string{fmt.Sprintf("%s case %s: got %d type-switch consumer clauses, want exactly 1", owner, message, len(clauses))}
	}
	clause := clauses[0]
	if got := countCalls(clause, "acceptAsync"); got != 1 {
		return []string{fmt.Sprintf("%s case %s: got %d acceptAsync calls, want exactly 1 rejecting shared-predicate guard", owner, message, got)}
	}

	guardIndex := -1
	for i, statement := range clause.Body {
		ifStmt, ok := statement.(*ast.IfStmt)
		if !ok || countCalls(ifStmt.Cond, "acceptAsync") != 1 {
			continue
		}
		if !conditionRejectsAccept(ifStmt.Cond) || !blockEndsInReturn(ifStmt.Body) {
			return []string{fmt.Sprintf("%s case %s: acceptAsync must be a fail-closed early-return guard", owner, message)}
		}
		guardIndex = i
		break
	}
	if guardIndex < 0 {
		return []string{fmt.Sprintf("%s case %s: acceptAsync is not a top-level rejecting guard that dominates later mutation", owner, message)}
	}
	for _, statement := range clause.Body[:guardIndex] {
		if hasDirectStateMutation(statement) {
			return []string{fmt.Sprintf("%s case %s: visible selector/index mutation occurs before acceptAsync; only exact-token non-publishing settlement may precede the guard", owner, message)}
		}
	}
	return nil
}

func (inv *asyncSourceInventory) refreshSettlementIssues() []string {
	const owner = "ProjectMailStore.settleRefreshWork"
	fn := inv.findFunction(owner)
	if fn == nil {
		return []string{owner + ": exact-token non-publishing settlement function missing"}
	}

	var issues []string
	if len(fn.Body.List) == 0 {
		return []string{owner + ": empty function cannot settle the exact physical token"}
	}
	guard, ok := fn.Body.List[0].(*ast.IfStmt)
	if !ok || guard.Init != nil || guard.Else != nil || countExactEnvelopeComparisons(guard.Cond) != 1 || !blockReturnsBoolean(guard.Body, "false") {
		issues = append(issues, owner+": first statement must be a fail-closed guard containing exactly one envelope != s.refreshInFlightEnvelope comparison")
	}

	allowedWrites := map[string]bool{
		"refreshInFlight":         true,
		"refreshInitial":          true,
		"refreshInFlightEnvelope": true,
	}
	writeCounts := make(map[string]int, len(allowedWrites))
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.IncDecStmt:
			issues = append(issues, owner+": increment/decrement mutation is forbidden; settlement may only clear the three exact in-flight bookkeeping fields")
		case *ast.AssignStmt:
			if len(node.Lhs) != len(node.Rhs) {
				issues = append(issues, owner+": settlement assignments must pair each bookkeeping field with its explicit zero value")
			}
			for i, lhs := range node.Lhs {
				selector, ok := lhs.(*ast.SelectorExpr)
				if !ok || !isNamedSelector(selector, "s", selector.Sel.Name) || !allowedWrites[selector.Sel.Name] {
					issues = append(issues, owner+": assignment outside s.refreshInFlight, s.refreshInitial, or s.refreshInFlightEnvelope is forbidden")
					continue
				}
				writeCounts[selector.Sel.Name]++
				if i >= len(node.Rhs) || !isSettlementZeroValue(selector.Sel.Name, node.Rhs[i]) {
					issues = append(issues, fmt.Sprintf("%s: s.%s must be assigned its explicit zero value", owner, selector.Sel.Name))
				}
			}
		}
		return true
	})
	for field := range allowedWrites {
		if writeCounts[field] != 1 {
			issues = append(issues, fmt.Sprintf("%s: s.%s must be cleared exactly once; got %d writes", owner, field, writeCounts[field]))
		}
	}
	if calls := calledNames(fn.Body); len(calls) != 0 {
		issues = append(issues, fmt.Sprintf("%s: helper/side-effect calls are forbidden (including installRefresh and the location updater); got %v", owner, calls))
	}
	if !blockReturnsBoolean(fn.Body, "true") {
		issues = append(issues, owner+": successful exact settlement must end by returning true")
	}
	return issues
}

func (inv *asyncSourceInventory) refreshSettlementOrderingIssues() []string {
	const owner = "App.Update"
	const message = "projectMailRefreshMsg"
	fn := inv.findFunction(owner)
	if fn == nil {
		return []string{owner + ": refresh-result consumer missing"}
	}
	clauses := typeSwitchClauses(fn, message)
	if len(clauses) != 1 {
		return []string{fmt.Sprintf("%s case %s: got %d clauses, want exactly 1", owner, message, len(clauses))}
	}
	clause := clauses[0]
	guardIndex := -1
	for i, statement := range clause.Body {
		ifStmt, ok := statement.(*ast.IfStmt)
		if ok && countCalls(ifStmt.Cond, "acceptAsync") == 1 {
			guardIndex = i
			break
		}
	}
	if guardIndex != 1 {
		return []string{fmt.Sprintf("%s case %s: settleRefreshWork assignment must be the sole statement before acceptAsync; guard index is %d", owner, message, guardIndex)}
	}

	assignment, ok := clause.Body[0].(*ast.AssignStmt)
	if !ok || assignment.Tok != token.DEFINE || len(assignment.Lhs) != 1 || len(assignment.Rhs) != 1 {
		return []string{owner + " case " + message + ": sole pre-accept statement must be settled := a.mailStore.settleRefreshWork(msg.envelope)"}
	}
	settled, ok := assignment.Lhs[0].(*ast.Ident)
	call, callOK := assignment.Rhs[0].(*ast.CallExpr)
	if !ok || settled.Name != "settled" || !callOK || !isMethodCallOnSelector(call, "a", "mailStore", "settleRefreshWork") || len(call.Args) != 1 || !isNamedSelector(call.Args[0], "msg", "envelope") {
		return []string{owner + " case " + message + ": sole pre-accept statement must bind only a.mailStore.settleRefreshWork(msg.envelope)"}
	}
	return nil
}

func countExactEnvelopeComparisons(node ast.Node) int {
	count := 0
	ast.Inspect(node, func(child ast.Node) bool {
		comparison, ok := child.(*ast.BinaryExpr)
		if !ok || comparison.Op != token.NEQ {
			return true
		}
		leftEnvelope, leftToken := isNamedIdentifier(comparison.X, "envelope"), isNamedSelector(comparison.X, "s", "refreshInFlightEnvelope")
		rightEnvelope, rightToken := isNamedIdentifier(comparison.Y, "envelope"), isNamedSelector(comparison.Y, "s", "refreshInFlightEnvelope")
		if (leftEnvelope && rightToken) || (leftToken && rightEnvelope) {
			count++
		}
		return true
	})
	return count
}

func isNamedIdentifier(expr ast.Expr, name string) bool {
	ident, ok := expr.(*ast.Ident)
	return ok && ident.Name == name
}

func isNamedSelector(expr ast.Expr, receiver, field string) bool {
	selector, ok := expr.(*ast.SelectorExpr)
	return ok && selector.Sel.Name == field && isNamedIdentifier(selector.X, receiver)
}

func isMethodCallOnSelector(call *ast.CallExpr, root, receiver, method string) bool {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	return ok && selector.Sel.Name == method && isNamedSelector(selector.X, root, receiver)
}

func isSettlementZeroValue(field string, expr ast.Expr) bool {
	switch field {
	case "refreshInFlight", "refreshInitial":
		return isNamedIdentifier(expr, "false")
	case "refreshInFlightEnvelope":
		literal, ok := expr.(*ast.CompositeLit)
		return ok && expressionName(literal.Type) == "asyncEnvelope" && len(literal.Elts) == 0
	default:
		return false
	}
}

func blockReturnsBoolean(block *ast.BlockStmt, value string) bool {
	if block == nil || len(block.List) == 0 {
		return false
	}
	result, ok := block.List[len(block.List)-1].(*ast.ReturnStmt)
	if !ok || len(result.Results) != 1 {
		return false
	}
	ident, ok := result.Results[0].(*ast.Ident)
	return ok && ident.Name == value
}

func typeSwitchClauses(fn *ast.FuncDecl, message string) []*ast.CaseClause {
	var clauses []*ast.CaseClause
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		typeSwitch, ok := node.(*ast.TypeSwitchStmt)
		if !ok {
			return true
		}
		for _, statement := range typeSwitch.Body.List {
			clause, ok := statement.(*ast.CaseClause)
			if !ok {
				continue
			}
			for _, expr := range clause.List {
				if expressionName(expr) == message {
					clauses = append(clauses, clause)
					break
				}
			}
		}
		return true
	})
	return clauses
}

func conditionRejectsAccept(expr ast.Expr) bool {
	switch expr := expr.(type) {
	case *ast.ParenExpr:
		return conditionRejectsAccept(expr.X)
	case *ast.UnaryExpr:
		return expr.Op == token.NOT && countCalls(expr.X, "acceptAsync") == 1
	case *ast.BinaryExpr:
		leftCalls := countCalls(expr.X, "acceptAsync") == 1
		rightCalls := countCalls(expr.Y, "acceptAsync") == 1
		leftBool := booleanIdentifier(expr.X)
		rightBool := booleanIdentifier(expr.Y)
		return (leftCalls && ((expr.Op == token.EQL && rightBool == "false") || (expr.Op == token.NEQ && rightBool == "true"))) ||
			(rightCalls && ((expr.Op == token.EQL && leftBool == "false") || (expr.Op == token.NEQ && leftBool == "true")))
	default:
		return false
	}
}

func booleanIdentifier(expr ast.Expr) string {
	if ident, ok := expr.(*ast.Ident); ok && (ident.Name == "true" || ident.Name == "false") {
		return ident.Name
	}
	return ""
}

func blockEndsInReturn(block *ast.BlockStmt) bool {
	return block != nil && len(block.List) != 0 && isTerminatingStatement(block.List[len(block.List)-1])
}

func isTerminatingStatement(statement ast.Stmt) bool {
	switch statement := statement.(type) {
	case *ast.ReturnStmt:
		return true
	case *ast.BlockStmt:
		return blockEndsInReturn(statement)
	default:
		return false
	}
}

func hasDirectStateMutation(node ast.Node) bool {
	mutates := false
	ast.Inspect(node, func(child ast.Node) bool {
		switch child := child.(type) {
		case *ast.IncDecStmt:
			mutates = true
			return false
		case *ast.AssignStmt:
			for _, lhs := range child.Lhs {
				switch lhs.(type) {
				case *ast.SelectorExpr, *ast.IndexExpr, *ast.IndexListExpr:
					mutates = true
					return false
				}
			}
		}
		return !mutates
	})
	return mutates
}

func (inv *asyncSourceInventory) findFunction(owner string) *ast.FuncDecl {
	for _, fn := range inv.funcs {
		if functionOwner(fn) == owner {
			return fn
		}
	}
	return nil
}

func (inv *asyncSourceInventory) findFunctionsBySimpleName(name string) []*ast.FuncDecl {
	var funcs []*ast.FuncDecl
	for _, fn := range inv.funcs {
		if fn.Name.Name == name {
			funcs = append(funcs, fn)
		}
	}
	return funcs
}

func functionOwner(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return fn.Name.Name
	}
	return expressionName(fn.Recv.List[0].Type) + "." + fn.Name.Name
}

func functionReturnsType(fn *ast.FuncDecl, typeName string) bool {
	return fn.Type.Results != nil && countFieldsOfType(fn.Type.Results, typeName) != 0
}

func countFieldsOfType(fields *ast.FieldList, typeName string) int {
	if fields == nil {
		return 0
	}
	count := 0
	for _, field := range fields.List {
		if expressionName(field.Type) != typeName {
			continue
		}
		if len(field.Names) == 0 {
			count++
		} else {
			count += len(field.Names)
		}
	}
	return count
}

func countCalls(node ast.Node, name string) int {
	if node == nil {
		return 0
	}
	count := 0
	ast.Inspect(node, func(child ast.Node) bool {
		call, ok := child.(*ast.CallExpr)
		if ok && calledName(call.Fun) == name {
			count++
		}
		return true
	})
	return count
}

func calledNames(node ast.Node) []string {
	var names []string
	if node == nil {
		return names
	}
	ast.Inspect(node, func(child ast.Node) bool {
		if call, ok := child.(*ast.CallExpr); ok {
			if name := calledName(call.Fun); name != "" {
				names = append(names, name)
			}
		}
		return true
	})
	return uniqueStrings(names)
}

func calledName(expr ast.Expr) string {
	switch expr := expr.(type) {
	case *ast.Ident:
		return expr.Name
	case *ast.SelectorExpr:
		return expr.Sel.Name
	case *ast.IndexExpr:
		return calledName(expr.X)
	case *ast.IndexListExpr:
		return calledName(expr.X)
	default:
		return ""
	}
}

func usesIdentifier(node ast.Node, name string) bool {
	found := false
	ast.Inspect(node, func(child ast.Node) bool {
		if ident, ok := child.(*ast.Ident); ok && ident.Name == name {
			found = true
			return false
		}
		return !found
	})
	return found
}

func expressionName(expr ast.Expr) string {
	switch expr := expr.(type) {
	case *ast.Ident:
		return expr.Name
	case *ast.StarExpr:
		return expressionName(expr.X)
	case *ast.SelectorExpr:
		return expr.Sel.Name
	case *ast.IndexExpr:
		return expressionName(expr.X)
	case *ast.IndexListExpr:
		return expressionName(expr.X)
	case *ast.ParenExpr:
		return expressionName(expr.X)
	default:
		return ""
	}
}

func sortedFileNames(files map[string]*ast.File) []string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func setDifference(left, right []string) []string {
	rightSet := make(map[string]bool, len(right))
	for _, item := range right {
		rightSet[item] = true
	}
	var difference []string
	for _, item := range left {
		if !rightSet[item] {
			difference = append(difference, item)
		}
	}
	return uniqueStrings(difference)
}

func uniqueStrings(values []string) []string {
	set := make(map[string]bool, len(values))
	for _, value := range values {
		set[value] = true
	}
	unique := make([]string, 0, len(set))
	for value := range set {
		unique = append(unique, value)
	}
	sort.Strings(unique)
	return unique
}
