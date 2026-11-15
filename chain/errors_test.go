package chain

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestMatchErrStr checks that `matchErrStr` can correctly replace the dashes
// with spaces and turn title cases into lowercases for a given error and match
// it against the specified string pattern.
func TestMatchErrStr(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		err      error
		matchStr string
		matched  bool
	}{
		{
			name:     "error without dashes",
			err:      errors.New("missing input"),
			matchStr: "missing input",
			matched:  true,
		},
		{
			name:     "match str without dashes",
			err:      errors.New("missing-input"),
			matchStr: "missing input",
			matched:  true,
		},
		{
			name:     "error with dashes",
			err:      errors.New("missing-input"),
			matchStr: "missing input",
			matched:  true,
		},
		{
			name:     "match str with dashes",
			err:      errors.New("missing-input"),
			matchStr: "missing-input",
			matched:  true,
		},
		{
			name:     "error with title case and dash",
			err:      errors.New("Missing-Input"),
			matchStr: "missing input",
			matched:  true,
		},
		{
			name:     "match str with title case and dash",
			err:      errors.New("missing-input"),
			matchStr: "Missing-Input",
			matched:  true,
		},
		{
			name:     "unmatched error",
			err:      errors.New("missing input"),
			matchStr: "missingorspent",
			matched:  false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			matched := matchErrStr(tc.err, tc.matchStr)
			require.Equal(t, tc.matched, matched)
		})
	}
}

// TestErrorSentinel checks that all defined RPCErr errors are added to the
// method `Error`.
func TestErrorSentinel(t *testing.T) {
	t.Parallel()

	rt := require.New(t)

	for i := uint32(0); i < uint32(errSentinel); i++ {
		err := RPCErr(i)
		rt.NotEqualf(err.Error(), "unknown error", "error code %d is "+
			"not defined, make sure to update it inside the Error "+
			"method", i)
	}
}
