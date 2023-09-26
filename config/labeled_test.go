package config

import (
	"testing"

	"golang.ngrok.com/ngrok/internal/tunnel/proto"
)

func TestLabeled(t *testing.T) {
	cases := testCases[labeledOptions, proto.LabelOptions]{
		{
			name: "simple",
			opts: LabeledTunnel(WithLabel("foo", "bar")),
			expectLabels: labelPtr(map[string]*string{
				"foo": stringPtr("bar"),
			}),
			expectProto:   stringPtr(""),
			expectNilOpts: true,
		},
		{
			name: "multiple",
			opts: LabeledTunnel(
				WithLabel("foo", "bar"),
				WithLabel("spam", "eggs"),
			),
			expectProto: stringPtr(""),
			expectLabels: labelPtr(map[string]*string{
				"foo":  stringPtr("bar"),
				"spam": stringPtr("eggs"),
			}),
			expectNilOpts: true,
		},
		{
			name: "withForwardsTo",
			opts: LabeledTunnel(WithLabel("foo", "bar"), WithForwardsTo("localhost:8080")),
			expectLabels: labelPtr(map[string]*string{
				"foo": stringPtr("bar"),
			}),
			expectForwardsTo: stringPtr("localhost:8080"),
			expectProto:      stringPtr(""),
			expectNilOpts:    true,
		},
		{
			name: "withMetadata",
			opts: LabeledTunnel(WithLabel("foo", "bar"), WithMetadata("choochoo")),
			expectLabels: labelPtr(map[string]*string{
				"foo": stringPtr("bar"),
			}),
			expectExtra: &matchBindExtra{
				Metadata: stringPtr("choochoo"),
			},
			expectProto:   stringPtr(""),
			expectNilOpts: true,
		},
	}

	cases.runAll(t)
}
