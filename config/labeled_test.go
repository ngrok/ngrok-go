package config

import (
	"testing"

	"golang.ngrok.com/ngrok/internal/tunnel/proto"
)

func TestLabeled(t *testing.T) {
	cases := testCases[*labeledOptions, proto.LabelOptions]{
		{
			name: "simple",
			opts: LabeledTunnel(WithLabel("foo", "bar")),
			expectLabels: ptr(map[string]*string{
				"foo": ptr("bar"),
			}),
			expectProto:   ptr(""),
			expectNilOpts: true,
		},
		{
			name: "multiple",
			opts: LabeledTunnel(
				WithLabel("foo", "bar"),
				WithLabel("spam", "eggs"),
			),
			expectProto: ptr(""),
			expectLabels: ptr(map[string]*string{
				"foo":  ptr("bar"),
				"spam": ptr("eggs"),
			}),
			expectNilOpts: true,
		},
		{
			name: "withForwardsTo",
			opts: LabeledTunnel(WithLabel("foo", "bar"), WithForwardsTo("localhost:8080")),
			expectLabels: ptr(map[string]*string{
				"foo": ptr("bar"),
			}),
			expectForwardsTo: ptr("localhost:8080"),
			expectProto:      ptr(""),
			expectNilOpts:    true,
		},
		{
			name: "withMetadata",
			opts: LabeledTunnel(WithLabel("foo", "bar"), WithMetadata("choochoo")),
			expectLabels: ptr(map[string]*string{
				"foo": ptr("bar"),
			}),
			expectExtra: &matchBindExtra{
				Metadata: ptr("choochoo"),
			},
			expectProto:   ptr(""),
			expectNilOpts: true,
		},
	}

	cases.runAll(t)
}
