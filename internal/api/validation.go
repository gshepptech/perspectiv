package api

import (
	"context"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	"perspectiv/internal/api/apiutil"
)

// ownPackage is the proto package whose enums reserve 0 for *_UNSPECIFIED.
// Well-known types (google.protobuf.NullValue) use 0 as a real value.
const ownPackage protoreflect.FullName = "perspectiv.v1"

// validator is implemented by a request message that checks itself.
type validator interface {
	Validate() error
}

// validationInterceptor is the innermost interceptor: it rejects requests no
// handler should see. It checks what holds for every RPC; each handler still
// validates its own fields before calling its service.
//
//   - A request message that implements Validate() error is checked with it;
//     a failure that is not already a typed apiutil error is InvalidArg.
//   - An enum value 0 (*_UNSPECIFIED) is always invalid input. Proto3 cannot
//     tell an unset implicit-presence enum from 0, so this rejects 0 wherever
//     it is visibly present: an optional (explicit presence) field set to 0,
//     a repeated element, or a map value, at any depth. A required enum left
//     unset is the handler's check, because many request enums are optional
//     filters where unset means "any".
type validationInterceptor struct{}

func validate(msg any) error {
	if v, ok := msg.(validator); ok {
		if err := v.Validate(); err != nil {
			if _, typed := apiutil.CodeOf(err); typed {
				return apiutil.ToConnect(err)
			}
			return apiutil.ToConnect(apiutil.InvalidArg(err.Error()))
		}
	}
	if m, ok := msg.(proto.Message); ok {
		if field, bad := unspecifiedEnum(m.ProtoReflect()); bad {
			return apiutil.ToConnect(apiutil.InvalidArg(field + " must be set"))
		}
	}
	return nil
}

// unspecifiedEnum returns the name of the first present enum value that is 0.
func unspecifiedEnum(m protoreflect.Message) (string, bool) {
	var name string
	var bad bool
	m.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		name, bad = checkField(fd, v)
		return !bad
	})
	return name, bad
}

func checkField(fd protoreflect.FieldDescriptor, v protoreflect.Value) (string, bool) {
	field := string(fd.Name())
	switch {
	case fd.IsList():
		l := v.List()
		for i := range l.Len() {
			if name, bad := checkValue(fd, l.Get(i)); bad {
				return joinField(field, name), true
			}
		}
	case fd.IsMap():
		var name string
		var bad bool
		v.Map().Range(func(_ protoreflect.MapKey, mv protoreflect.Value) bool {
			name, bad = checkValue(fd.MapValue(), mv)
			return !bad
		})
		if bad {
			return joinField(field, name), true
		}
	default:
		if name, bad := checkValue(fd, v); bad {
			return joinField(field, name), true
		}
	}
	return "", false
}

func checkValue(fd protoreflect.FieldDescriptor, v protoreflect.Value) (string, bool) {
	switch fd.Kind() {
	case protoreflect.EnumKind:
		return "", v.Enum() == 0 && fd.Enum().ParentFile().Package() == ownPackage
	case protoreflect.MessageKind, protoreflect.GroupKind:
		return unspecifiedEnum(v.Message())
	}
	return "", false
}

func joinField(parent, child string) string {
	if child == "" {
		return parent
	}
	return parent + "." + child
}

func (validationInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		if req.Spec().IsClient {
			return next(ctx, req)
		}
		if err := validate(req.Any()); err != nil {
			return nil, err
		}
		return next(ctx, req)
	}
}

func (validationInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

// WrapStreamingHandler validates each received message of a streaming call.
func (validationInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		return next(ctx, validatingConn{conn})
	}
}

type validatingConn struct {
	connect.StreamingHandlerConn
}

func (c validatingConn) Receive(msg any) error {
	if err := c.StreamingHandlerConn.Receive(msg); err != nil {
		return err
	}
	return validate(msg)
}
