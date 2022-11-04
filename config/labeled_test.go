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
			name: "mulitple",
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
	}

	cases.runAll(t)
}
