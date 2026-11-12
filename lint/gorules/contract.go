package gorules

import (
	"go/ast"
	"go/constant"
	"go/types"
	"regexp"
	"strings"

	"golang.org/x/tools/go/analysis"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"

	dedov1 "github.com/raveesh-me/dedo/gen/go/dedo/v1"
)

// rawDescName is the variable or constant protoc-gen-go stores a file's
// serialized descriptor in.
var rawDescName = regexp.MustCompile(`^file_.*_rawDesc$`)

// contract is what the .proto files generated into one Go package declare,
// read from their descriptors as package policy reads them at startup.
type contract struct {
	// Messages are the package's messages, a slice rather than a map so
	// the fact encodes the same way every time.
	Messages []message
	// Methods are the unary RPCs of the services that declare
	// dedo.v1.service.
	Methods []rpc
}

// message is one message's Go type name in its package and its full name.
type message struct{ GoName, FullName string }

// messageNamed returns the full name of the message whose Go type is name.
func (c contract) messageNamed(name string) (string, bool) {
	for _, m := range c.Messages {
		if m.GoName == name {
			return m.FullName, true
		}
	}
	return "", false
}

// rpc is one RPC's declaration.
type rpc struct {
	// GoName is the name of its handler method.
	GoName string
	// Procedure is /<service full name>/<method>.
	Procedure string
	// Input and Output are full names of messages.
	Input, Output string
	Reasons       []string
	Emits         []string
}

// readContract reads the descriptors generated into the package, if any.
func readContract(pass *analysis.Pass) (contract, bool) {
	var c contract
	found := false
	for _, f := range pass.Files {
		for _, raw := range rawDescs(pass, f) {
			var fd descriptorpb.FileDescriptorProto
			if err := proto.Unmarshal(raw, &fd); err != nil {
				continue // not a descriptor after all
			}
			found = true
			addFile(&c, &fd)
		}
	}
	return c, found
}

// rawDescs returns the serialized descriptors a generated file holds: a
// string constant since protoc-gen-go v1.36.1, a byte slice before.
func rawDescs(pass *analysis.Pass, f *ast.File) [][]byte {
	var out [][]byte
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok || len(vs.Names) != 1 || !rawDescName.MatchString(vs.Names[0].Name) {
				continue
			}
			if b, ok := rawBytes(pass, vs); ok {
				out = append(out, b)
			}
		}
	}
	return out
}

func rawBytes(pass *analysis.Pass, vs *ast.ValueSpec) ([]byte, bool) {
	if c, ok := pass.TypesInfo.Defs[vs.Names[0]].(*types.Const); ok {
		if c.Val().Kind() == constant.String {
			return []byte(constant.StringVal(c.Val())), true
		}
		return nil, false
	}
	if len(vs.Values) != 1 {
		return nil, false
	}
	lit, ok := ast.Unparen(vs.Values[0]).(*ast.CompositeLit)
	if !ok {
		return nil, false
	}
	b := make([]byte, 0, len(lit.Elts))
	for _, e := range lit.Elts {
		tv := pass.TypesInfo.Types[e]
		if tv.Value == nil || tv.Value.Kind() != constant.Int {
			return nil, false
		}
		v, ok := constant.Int64Val(tv.Value)
		if !ok || v < 0 || v > 255 {
			return nil, false
		}
		b = append(b, byte(v))
	}
	return b, true
}

// addFile adds one file's messages and dedo RPCs.
func addFile(c *contract, fd *descriptorpb.FileDescriptorProto) {
	pkg := fd.GetPackage()
	var addMessages func(prefix string, msgs []*descriptorpb.DescriptorProto)
	addMessages = func(prefix string, msgs []*descriptorpb.DescriptorProto) {
		for _, m := range msgs {
			name := prefix + m.GetName()
			c.Messages = append(c.Messages, message{GoName: goCamelCase(name), FullName: qualify(pkg, name)})
			addMessages(name+".", m.GetNestedType())
		}
	}
	addMessages("", fd.GetMessageType())
	for _, svc := range fd.GetService() {
		if !proto.HasExtension(svc.GetOptions(), dedov1.E_Service) {
			continue // not a dedo service: policy.Build would refuse it
		}
		for _, m := range svc.GetMethod() {
			if m.GetClientStreaming() || m.GetServerStreaming() {
				continue
			}
			rules, _ := proto.GetExtension(m.GetOptions(), dedov1.E_Method).(*dedov1.MethodRules)
			r := rpc{
				GoName:    goCamelCase(m.GetName()),
				Procedure: "/" + qualify(pkg, svc.GetName()) + "/" + m.GetName(),
				Input:     strings.TrimPrefix(m.GetInputType(), "."),
				Output:    strings.TrimPrefix(m.GetOutputType(), "."),
				Emits:     rules.GetEmits(),
			}
			for _, e := range rules.GetErrors() {
				r.Reasons = append(r.Reasons, e.GetReason())
			}
			c.Methods = append(c.Methods, r)
		}
	}
}

func qualify(pkg, name string) string {
	if pkg == "" {
		return name
	}
	return pkg + "." + name
}

// goCamelCase is the Go name protoc-gen-go gives a proto name, as
// google.golang.org/protobuf/internal/strs.GoCamelCase computes it
// (BSD-3-Clause, The Go Authors).
func goCamelCase(s string) string {
	var b []byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '.' && i+1 < len(s) && isLower(s[i+1]):
			// Skip over '.' in ".{{lowercase}}".
		case c == '.':
			b = append(b, '_')
		case c == '_' && (i == 0 || s[i-1] == '.'):
			b = append(b, 'X')
		case c == '_' && i+1 < len(s) && isLower(s[i+1]):
			// Skip over '_' in "_{{lowercase}}".
		case c >= '0' && c <= '9':
			b = append(b, c)
		default:
			if isLower(c) {
				c -= 'a' - 'A'
			}
			b = append(b, c)
			for ; i+1 < len(s) && isLower(s[i+1]); i++ {
				b = append(b, s[i+1])
			}
		}
	}
	return string(b)
}

func isLower(c byte) bool { return c >= 'a' && c <= 'z' }
