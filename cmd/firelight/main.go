package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/MoonlightByte/firelight/compiler"
	"github.com/MoonlightByte/firelight/core"
	"io"
	"os"
	"strings"
)

func main() { os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)) }
func emit(w io.Writer, v any) error {
	e := json.NewEncoder(w)
	e.SetIndent("", "  ")
	return e.Encode(v)
}
func sourceFile(path string, limit int64) (string, error) {
	f, e := os.Open(path)
	if e != nil {
		return "", e
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, limit+1))
	return string(b), e
}

type output struct {
	Revision core.Revision                   `json:"revision"`
	Result   *core.Batch                     `json:"result,omitempty"`
	World    *core.Snapshot                  `json:"world,omitempty"`
	Status   *core.CharacterStatusProjection `json:"status,omitempty"`
	Transfer *core.TransferProjection        `json:"transfer,omitempty"`
}

func run(args []string, in io.Reader, out, errOut io.Writer) int {
	f := flag.NewFlagSet("nql", flag.ContinueOnError)
	f.SetOutput(errOut)
	worldPath := f.String("world", "examples/micro-world.nql", "world declaration source")
	actionPath := f.String("execute", "", "execute one NQL transaction file")
	actor := f.String("actor", "", "explicit actor ID for action attribution")
	actorKind := f.String("actor-kind", "character", "actor entity kind")
	request := f.String("request", "cli:1", "unique request ID")
	status := f.String("status", "", "character status projection")
	transfer := f.String("transfer", "", "item transfer projection")
	to := f.String("to", "", "transfer projection recipient")
	inspect := f.Bool("inspect", false, "include complete detached world inspection")
	repl := f.Bool("repl", false, "line-oriented transactions; :inspect and :quit commands")
	runtimeErrors := f.String("runtime-errors", "legacy", "runtime error output: legacy or structured")
	if e := f.Parse(args); e != nil {
		return 2
	}
	if *runtimeErrors != "legacy" && *runtimeErrors != "structured" {
		fmt.Fprintf(errOut, "invalid value %q for -runtime-errors: expected legacy or structured\n", *runtimeErrors)
		f.Usage()
		return 2
	}
	if f.NArg() != 0 {
		fmt.Fprintln(errOut, "unexpected positional arguments")
		return 2
	}
	report := func(e error) int {
		_ = emit(errOut, struct {
			Error string `json:"error"`
		}{e.Error()})
		return 1
	}
	diagnostics := func(ds []compiler.Diagnostic) int {
		_ = emit(errOut, struct {
			Diagnostics []compiler.Diagnostic `json:"diagnostics"`
		}{ds})
		return 1
	}
	src, e := sourceFile(*worldPath, compiler.MaxWorldSourceBytes)
	if e != nil {
		return report(e)
	}
	b, ds := compiler.CompileWorld(*worldPath, src)
	if len(ds) > 0 {
		return diagnostics(ds)
	}
	w, e := core.NewWorld(b)
	if e != nil {
		return report(e)
	}
	if (*actionPath != "" || *repl) && *actor == "" {
		return report(fmt.Errorf("-actor is required for execution (attribution, not authorization)"))
	}
	if *repl && *actionPath != "" {
		return report(fmt.Errorf("choose -repl or -execute"))
	}
	execute := func(name, src, id string) (core.Batch, bool) {
		s := w.Snapshot()
		ops, ds := compiler.CompileActions(name, src, s.Blueprint)
		if len(ds) > 0 {
			diagnostics(ds)
			return core.Batch{}, false
		}
		batch, e := w.Execute(core.Request{ID: id, ExpectedRevision: s.Revision, Actor: core.Ref{Kind: core.EntityKind(*actorKind), ID: *actor}, Operations: ops})
		if e != nil {
			_ = emit(errOut, runtimeErrorResponse(e, *runtimeErrors == "structured"))
			return core.Batch{}, false
		}
		return batch, true
	}
	if *repl {
		scanner := bufio.NewScanner(in)
		scanner.Buffer(make([]byte, 4096), compiler.MaxSourceBytes+1)
		n := 0
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			if line == ":quit" {
				break
			}
			if line == ":inspect" {
				s := w.Snapshot()
				if e := emit(out, output{Revision: s.Revision, World: &s}); e != nil {
					return report(e)
				}
				continue
			}
			n++
			batch, ok := execute(fmt.Sprintf("<stdin:%d>", n), line, fmt.Sprintf("%s:%d", *request, n))
			if ok {
				if e := emit(out, output{Revision: batch.Revision, Result: &batch}); e != nil {
					return report(e)
				}
			}
		}
		if e := scanner.Err(); e != nil {
			return report(e)
		}
		return 0
	}
	result := output{}
	if *actionPath != "" {
		src, e := sourceFile(*actionPath, compiler.MaxSourceBytes)
		if e != nil {
			return report(e)
		}
		batch, ok := execute(*actionPath, src, *request)
		if !ok {
			return 1
		}
		result.Result = &batch
	}
	if *status != "" {
		p, e := w.CharacterStatus(core.CharacterID(*status))
		if e != nil {
			return report(e)
		}
		result.Status = &p
	}
	if *transfer != "" {
		p, e := w.TransferView(core.ItemID(*transfer), core.CharacterID(*to))
		if e != nil {
			return report(e)
		}
		result.Transfer = &p
	}
	if *inspect || (*status == "" && *transfer == "" && *actionPath == "") {
		s := w.Snapshot()
		result.World = &s
	}
	result.Revision = w.Revision()
	if e := emit(out, result); e != nil {
		return report(e)
	}
	return 0
}
