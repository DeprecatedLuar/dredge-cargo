package editor

import (
	"testing"
)

func TestParseTemplate(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantTitle   string
		wantContent string
		wantTags    []string
		wantErr     bool
	}{
		{
			name:        "full template with content",
			input:       "my title #tag1 #tag2\n\nContent here\nMultiple lines",
			wantTitle:   "my title",
			wantContent: "Content here\nMultiple lines",
			wantTags:    []string{"tag1", "tag2"},
			wantErr:     false,
		},
		{
			name:        "title only (no content)",
			input:       "just a title",
			wantTitle:   "just a title",
			wantContent: "",
			wantTags:    nil,
			wantErr:     false,
		},
		{
			name:        "title with tags, no content",
			input:       "title #tag1 #tag2",
			wantTitle:   "title",
			wantContent: "",
			wantTags:    []string{"tag1", "tag2"},
			wantErr:     false,
		},
		{
			name:        "empty input",
			input:       "",
			wantTitle:   "",
			wantContent: "",
			wantTags:    nil,
			wantErr:     true,
		},
		{
			name:        "blank line only",
			input:       "\n",
			wantTitle:   "",
			wantContent: "",
			wantTags:    nil,
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotTitle, gotContent, gotTags, err := parseTemplate(tt.input)

			if (err != nil) != tt.wantErr {
				t.Errorf("parseTemplate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if err != nil {
				return // Expected error, skip other checks
			}

			if gotTitle != tt.wantTitle {
				t.Errorf("parseTemplate() title = %q, want %q", gotTitle, tt.wantTitle)
			}

			if gotContent != tt.wantContent {
				t.Errorf("parseTemplate() content = %q, want %q", gotContent, tt.wantContent)
			}

			if len(gotTags) != len(tt.wantTags) {
				t.Errorf("parseTemplate() tags length = %d, want %d", len(gotTags), len(tt.wantTags))
				return
			}

			for i, tag := range gotTags {
				if tag != tt.wantTags[i] {
					t.Errorf("parseTemplate() tag[%d] = %q, want %q", i, tag, tt.wantTags[i])
				}
			}
		})
	}
}

func TestParseTitleAndTags(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantTitle string
		wantTags  []string
	}{
		{
			name:      "title with trailing tags",
			input:     "github api key #ssh #api #credentials",
			wantTitle: "github api key",
			wantTags:  []string{"ssh", "api", "credentials"},
		},
		{
			name:      "title only, no tags",
			input:     "just a title",
			wantTitle: "just a title",
			wantTags:  nil,
		},
		{
			name:      "hashtag in middle with text after",
			input:     "text about #hashtags so this",
			wantTitle: "text about #hashtags so this",
			wantTags:  nil,
		},
		{
			name:      "hashtag followed by non-hashtag word",
			input:     "title #tag 2",
			wantTitle: "title #tag 2",
			wantTags:  nil,
		},
		{
			name:      "single tag",
			input:     "title #tag",
			wantTitle: "title",
			wantTags:  []string{"tag"},
		},
		{
			name:      "tags with trailing whitespace",
			input:     "title #tag1 #tag2   ",
			wantTitle: "title",
			wantTags:  []string{"tag1", "tag2"},
		},
		{
			name:      "hashtag at start",
			input:     "#tag1 #tag2",
			wantTitle: "",
			wantTags:  []string{"tag1", "tag2"},
		},
		{
			name:      "multiple hashtags in middle",
			input:     "start #mid1 #mid2 end",
			wantTitle: "start #mid1 #mid2 end",
			wantTags:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotTitle, gotTags := parseTitleAndTags(tt.input)

			if gotTitle != tt.wantTitle {
				t.Errorf("parseTitleAndTags() title = %q, want %q", gotTitle, tt.wantTitle)
			}

			if len(gotTags) != len(tt.wantTags) {
				t.Errorf("parseTitleAndTags() tags length = %d, want %d", len(gotTags), len(tt.wantTags))
				return
			}

			for i, tag := range gotTags {
				if tag != tt.wantTags[i] {
					t.Errorf("parseTitleAndTags() tag[%d] = %q, want %q", i, tag, tt.wantTags[i])
				}
			}
		})
	}
}
