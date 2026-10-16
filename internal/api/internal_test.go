package api

import (
	"errors"
	"net/http"
	"testing"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
	"google.golang.org/protobuf/types/known/structpb"

	"perspectiv/internal/api/apiutil"
)

func TestClientIP(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		peer string
		xff  []string
		want string
	}{
		{name: "public peer ignores forwarded header", peer: "198.51.100.7:5555", xff: []string{"203.0.113.1"}, want: "198.51.100.7"},
		{name: "loopback peer without header", peer: "127.0.0.1:5555", want: "127.0.0.1"},
		{name: "loopback peer uses rightmost entry", peer: "127.0.0.1:5555", xff: []string{"1.1.1.1, 203.0.113.9"}, want: "203.0.113.9"},
		{name: "private peer uses last header line", peer: "10.42.0.7:5555", xff: []string{"1.1.1.1", "203.0.113.10"}, want: "203.0.113.10"},
		{name: "garbage forwarded entry falls back to peer", peer: "10.42.0.7:5555", xff: []string{"not-an-ip"}, want: "10.42.0.7"},
		{name: "ipv6 peer", peer: "[2001:db8::1]:443", want: "2001:db8::1"},
		{name: "mapped ipv4", peer: "[::ffff:198.51.100.7]:1", want: "198.51.100.7"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := http.Header{}
			for _, v := range tc.xff {
				h.Add("X-Forwarded-For", v)
			}
			if got := clientIP(tc.peer, h); got != tc.want {
				t.Fatalf("clientIP = %q, want %q", got, tc.want)
			}
		})
	}
}

// probeType builds a proto2 message in package perspectiv.v1 whose enum
// fields have explicit presence, so a 0 can be visibly set.
func probeType(t *testing.T) protoreflect.MessageType {
	t.Helper()
	opt := descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum()
	rep := descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum()
	enumT := descriptorpb.FieldDescriptorProto_TYPE_ENUM.Enum()
	msgT := descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum()
	fdp := &descriptorpb.FileDescriptorProto{
		Name:    proto.String("perspectiv/v1/validation_probe_test.proto"),
		Package: proto.String("perspectiv.v1"),
		Syntax:  proto.String("proto2"),
		EnumType: []*descriptorpb.EnumDescriptorProto{{
			Name: proto.String("ProbeKind"),
			Value: []*descriptorpb.EnumValueDescriptorProto{
				{Name: proto.String("PROBE_KIND_UNSPECIFIED"), Number: proto.Int32(0)},
				{Name: proto.String("PROBE_KIND_A"), Number: proto.Int32(1)},
			},
		}},
		MessageType: []*descriptorpb.DescriptorProto{{
			Name: proto.String("Probe"),
			Field: []*descriptorpb.FieldDescriptorProto{
				{Name: proto.String("one"), Number: proto.Int32(1), Label: opt, Type: enumT, TypeName: proto.String(".perspectiv.v1.ProbeKind")},
				{Name: proto.String("many"), Number: proto.Int32(2), Label: rep, Type: enumT, TypeName: proto.String(".perspectiv.v1.ProbeKind")},
				{Name: proto.String("child"), Number: proto.Int32(3), Label: opt, Type: msgT, TypeName: proto.String(".perspectiv.v1.Probe")},
			},
		}},
	}
	fd, err := protodesc.NewFile(fdp, nil)
	if err != nil {
		t.Fatalf("build probe descriptor: %v", err)
	}
	return dynamicpb.NewMessageType(fd.Messages().ByName("Probe"))
}

type selfValidating struct{ err error }

func (s selfValidating) Validate() error { return s.err }

func TestValidate(t *testing.T) {
	t.Parallel()
	mt := probeType(t)
	fields := mt.Descriptor().Fields()
	probe := func(set func(m protoreflect.Message)) proto.Message {
		m := mt.New()
		set(m)
		return m.Interface()
	}
	one, many, child := fields.ByName("one"), fields.ByName("many"), fields.ByName("child")

	tests := []struct {
		name     string
		msg      any
		wantCode connect.Code // 0 = valid
		wantMsg  string
	}{
		{name: "empty message", msg: probe(func(protoreflect.Message) {})},
		{name: "enum set to a real value", msg: probe(func(m protoreflect.Message) { m.Set(one, protoreflect.ValueOfEnum(1)) })},
		{name: "optional enum set to unspecified", msg: probe(func(m protoreflect.Message) { m.Set(one, protoreflect.ValueOfEnum(0)) }), wantCode: connect.CodeInvalidArgument, wantMsg: "one must be set"},
		{name: "repeated enum holding unspecified", msg: probe(func(m protoreflect.Message) {
			l := m.Mutable(many).List()
			l.Append(protoreflect.ValueOfEnum(1))
			l.Append(protoreflect.ValueOfEnum(0))
		}), wantCode: connect.CodeInvalidArgument, wantMsg: "many must be set"},
		{name: "nested unspecified", msg: probe(func(m protoreflect.Message) {
			m.Mutable(child).Message().Set(one, protoreflect.ValueOfEnum(0))
		}), wantCode: connect.CodeInvalidArgument, wantMsg: "child.one must be set"},
		{name: "well-known enum zero is a real value", msg: structpb.NewNullValue()},
		{name: "Validate failure is invalid argument", msg: selfValidating{err: errors.New("title too long")}, wantCode: connect.CodeInvalidArgument, wantMsg: "title too long"},
		{name: "Validate typed error keeps its code", msg: selfValidating{err: apiutil.FailedPrecondition("nope")}, wantCode: connect.CodeFailedPrecondition},
		{name: "Validate passes", msg: selfValidating{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := validate(tc.msg)
			if tc.wantCode == 0 {
				if err != nil {
					t.Fatalf("validate = %v, want nil", err)
				}
				return
			}
			var cerr *connect.Error
			if !errors.As(err, &cerr) || cerr.Code() != tc.wantCode {
				t.Fatalf("validate = %v, want code %v", err, tc.wantCode)
			}
			if tc.wantMsg != "" && cerr.Message() != tc.wantMsg {
				t.Fatalf("message = %q, want %q", cerr.Message(), tc.wantMsg)
			}
		})
	}
}
