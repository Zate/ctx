package token

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEstimate_EmptyString(t *testing.T) {
	assert.Equal(t, 0, Estimate(""))
}

func TestEstimate_ShortString(t *testing.T) {
	// 1-3 chars: len/4 == 0 but content > 0, so returns 1
	assert.Equal(t, 1, Estimate("hi"))
	assert.Equal(t, 1, Estimate("a"))
	assert.Equal(t, 1, Estimate("abc"))
}

func TestEstimate_FourChars(t *testing.T) {
	assert.Equal(t, 1, Estimate("abcd"))
}

func TestEstimate_LongerString(t *testing.T) {
	// 100 chars / 4 = 25
	s := strings.Repeat("a", 100)
	assert.Equal(t, 25, Estimate(s))
}

func TestEstimate_RealContent(t *testing.T) {
	content := "This is a realistic node content string that might be stored in ctx."
	result := Estimate(content)
	assert.Greater(t, result, 0)
	assert.Equal(t, len(content)/4, result)
}
