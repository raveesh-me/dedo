package house

import (
	"fmt"
	"sync"

	"buf.build/go/bufplugin/check"
	celtypes "github.com/google/cel-go/common/types"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

// The image a check reads, as the variable image: every file, service and
// method buf passes, the module's own and those of its imports, so a rule on
// one element can look across files. Its keys are files, services and
// methods, maps keyed by path or full name. Each entry holds the variables a
// rule on that element reads, options included, and imported, true for an
// import's element. A breaking rule reads the current version's image;
// against has none.

// imageCache holds the image of the request being checked, built once, the
// first time a check reads it.
var imageCache struct {
	sync.Mutex
	req check.Request
	img map[string]any
	err error
}

// lazyImage returns the image variable's value: CEL calls it only when a
// check reads image, so rules that do not pay nothing for it.
func lazyImage(req check.Request) func() any {
	return func() any {
		img, err := requestImage(req)
		if err != nil {
			return celtypes.NewErr("the image could not be read: %v", err)
		}
		return img
	}
}

func requestImage(req check.Request) (map[string]any, error) {
	imageCache.Lock()
	defer imageCache.Unlock()
	if imageCache.req != req {
		img, err := buildImage(req)
		imageCache.req, imageCache.img, imageCache.err = req, img, err
	}
	return imageCache.img, imageCache.err
}

func buildImage(req check.Request) (map[string]any, error) {
	extensions, err := requestTypes(req, false)
	if err != nil {
		return nil, fmt.Errorf("the request's extensions could not be read: %w", err)
	}
	files, services, methods := map[string]any{}, map[string]any{}, map[string]any{}
	for _, f := range req.FileDescriptors() {
		fd := f.ProtoreflectFileDescriptor()
		v, err := entry(fileVars(fd), fd, extensions, f.IsImport())
		if err != nil {
			return nil, err
		}
		files[fd.Path()] = v
		for i := range fd.Services().Len() {
			s := fd.Services().Get(i)
			if services[string(s.FullName())], err = entry(serviceVars(s), s, extensions, f.IsImport()); err != nil {
				return nil, err
			}
			for j := range s.Methods().Len() {
				m := s.Methods().Get(j)
				if methods[string(m.FullName())], err = entry(methodVars(m), m, extensions, f.IsImport()); err != nil {
					return nil, err
				}
			}
		}
	}
	return map[string]any{"files": files, "services": services, "methods": methods}, nil
}

// entry completes an element's variables with its options and imported.
func entry(vars map[string]any, d protoreflect.Descriptor, extensions *protoregistry.Types, imported bool) (map[string]any, error) {
	if err := withOptions(vars, d, extensions); err != nil {
		return nil, err
	}
	vars["imported"] = imported
	return vars, nil
}
