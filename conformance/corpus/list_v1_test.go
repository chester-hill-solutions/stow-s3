package corpus

import "testing"

func TestListV1CorpusRequiresTheCorrectCursorSource(t *testing.T) {
	for _, testCase := range []struct {
		delimiter string
		page      sharedCorpusPage
		invalid   bool
	}{
		{"", sharedCorpusPage{IsTruncated: true, Contents: []string{"key"}}, false},
		{"/", sharedCorpusPage{IsTruncated: true, NextMarker: "group/"}, false},
		{"", sharedCorpusPage{IsTruncated: true}, true},
		{"", sharedCorpusPage{IsTruncated: true, Contents: []string{"key"}, NextMarker: "key"}, true},
		{"/", sharedCorpusPage{IsTruncated: true}, true},
		{"/", sharedCorpusPage{NextMarker: "group/"}, true},
	} {
		if problem := listV1CursorProblem(testCase.delimiter, testCase.page); (problem != "") != testCase.invalid {
			t.Errorf("delimiter=%q page=%+v: %q", testCase.delimiter, testCase.page, problem)
		}
	}
}
