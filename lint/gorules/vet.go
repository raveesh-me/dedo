package gorules

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
)

// Finding is one diagnostic go vet reported.
type Finding struct {
	// Posn is file:line:column.
	Posn string `json:"posn"`
	// Category is the rule ID.
	Category string `json:"category"`
	Message  string `json:"message"`
}

func (f Finding) String() string { return f.Posn + ": " + f.Message }

// VetArgs returns the arguments of go vet that run the plan on patterns
// with tool, the dedo command: -json, so go vet caches clean packages
// and every finding arrives whole; the rules the plan leaves out turned
// off; and the house rules, with root, in the house analyzer's flag. bff
// checks a backend for frontend's servers, which need no policy
// interceptor.
func VetArgs(tool string, p Plan, root string, bff bool, patterns []string) ([]string, error) {
	args := []string{"vet", "-vettool=" + tool, "-json"}
	for _, b := range Builtins() {
		if !slices.Contains(p.Builtin, b.ID) {
			args = append(args, "-"+b.Analyzer.Name+"=false")
		}
	}
	if bff {
		args = append(args, "-"+ServerInterceptors.Name+".bff")
	}
	if len(p.House) > 0 {
		flag, err := HouseConfig{Root: root, Rules: p.House}.Flag()
		if err != nil {
			return nil, err
		}
		args = append(args, "-"+House.Name+".config="+flag)
	} else {
		args = append(args, "-"+House.Name+"=false")
	}
	return append(args, patterns...), nil
}

// ParseVet reads go vet -json's output: per package, a JSON object of
// diagnostics by analyzer, after a "# package" line on stderr. It returns the
// findings, sorted and without the copies a package's test variant
// repeats, and the analyzers' errors.
func ParseVet(r io.Reader) ([]Finding, []string, error) {
	var findings []Finding
	var errs []string
	body, err := skipComments(r)
	if err != nil {
		return nil, nil, fmt.Errorf("reading go vet's output: %w", err)
	}
	dec := json.NewDecoder(body)
	for {
		var tree map[string]map[string]json.RawMessage
		err := dec.Decode(&tree)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, fmt.Errorf("reading go vet's output: %w", err)
		}
		for pkg, byAnalyzer := range tree {
			for analyzer, raw := range byAnalyzer {
				var fs []Finding
				if err := json.Unmarshal(raw, &fs); err == nil {
					findings = append(findings, fs...)
					continue
				}
				var failed struct{ Error string }
				if err := json.Unmarshal(raw, &failed); err != nil || failed.Error == "" {
					if err == nil {
						err = fmt.Errorf("neither findings nor an error: %s", raw)
					}
					return nil, nil, fmt.Errorf("reading go vet's output for %s: %w", pkg, err)
				}
				errs = append(errs, fmt.Sprintf("%s: %s: %s", pkg, analyzer, failed.Error))
			}
		}
	}
	slices.SortFunc(findings, func(a, b Finding) int {
		if c := comparePosn(a.Posn, b.Posn); c != 0 {
			return c
		}
		return strings.Compare(a.Message, b.Message)
	})
	slices.Sort(errs)
	return slices.Compact(findings), slices.Compact(errs), nil
}

// skipComments keeps only the lines of the packages' JSON: it drops the
// "# package" lines go vet prints between them, and anything else a tool
// prints that is not JSON, such as a coverage-instrumented build's warning.
func skipComments(r io.Reader) (io.Reader, error) {
	var b bytes.Buffer
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 64*1024*1024)
	for sc.Scan() {
		if line := sc.Text(); line == "" || strings.ContainsRune("{}[]\" \t", rune(line[0])) {
			b.Write(sc.Bytes())
			b.WriteByte('\n')
		}
	}
	return &b, sc.Err()
}

// comparePosn orders file:line:column positions by file, then line and
// column as numbers.
func comparePosn(a, b string) int {
	af, al, ac := splitPosn(a)
	bf, bl, bc := splitPosn(b)
	if c := strings.Compare(af, bf); c != 0 {
		return c
	}
	if al != bl {
		return al - bl
	}
	return ac - bc
}

func splitPosn(p string) (string, int, int) {
	var line, col int
	parts := strings.Split(p, ":")
	if len(parts) < 3 {
		return p, 0, 0
	}
	line, _ = strconv.Atoi(parts[len(parts)-2])
	col, _ = strconv.Atoi(parts[len(parts)-1])
	return strings.Join(parts[:len(parts)-2], ":"), line, col
}
