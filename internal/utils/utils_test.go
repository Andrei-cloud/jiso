package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestResponseMTIAndRequestMTI(t *testing.T) {
	tests := []struct {
		mti          string
		expectedResp string
		expectedReq  string
		isResp       bool
	}{
		{mti: "0100", expectedResp: "0110", expectedReq: "0100", isResp: false},
		{mti: "0101", expectedResp: "0111", expectedReq: "0101", isResp: false},
		{mti: "0110", expectedResp: "0110", expectedReq: "0100", isResp: true},
		{mti: "0111", expectedResp: "0111", expectedReq: "0101", isResp: true},
		{mti: "0120", expectedResp: "0130", expectedReq: "0120", isResp: false},
		{mti: "0130", expectedResp: "0130", expectedReq: "0120", isResp: true},
		{mti: "0180", expectedResp: "0180", expectedReq: "0180", isResp: true},
		{mti: "0190", expectedResp: "0190", expectedReq: "0190", isResp: true},
		{mti: "0200", expectedResp: "0210", expectedReq: "0200", isResp: false},
		{mti: "0210", expectedResp: "0210", expectedReq: "0200", isResp: true},
		{mti: "0220", expectedResp: "0230", expectedReq: "0220", isResp: false},
		{mti: "0221", expectedResp: "0231", expectedReq: "0221", isResp: false},
		{mti: "0230", expectedResp: "0230", expectedReq: "0220", isResp: true},
		{mti: "0231", expectedResp: "0231", expectedReq: "0221", isResp: true},
		{mti: "0302", expectedResp: "0312", expectedReq: "0302", isResp: false},
		{mti: "0312", expectedResp: "0312", expectedReq: "0302", isResp: true},
		{mti: "0400", expectedResp: "0410", expectedReq: "0400", isResp: false},
		{mti: "0410", expectedResp: "0410", expectedReq: "0400", isResp: true},
		{mti: "0420", expectedResp: "0430", expectedReq: "0420", isResp: false},
		{mti: "0430", expectedResp: "0430", expectedReq: "0420", isResp: true},
		{mti: "0600", expectedResp: "0610", expectedReq: "0600", isResp: false},
		{mti: "0610", expectedResp: "0610", expectedReq: "0600", isResp: true},
		{mti: "0620", expectedResp: "0630", expectedReq: "0620", isResp: false},
		{mti: "0621", expectedResp: "0631", expectedReq: "0621", isResp: false},
		{mti: "0630", expectedResp: "0630", expectedReq: "0620", isResp: true},
		{mti: "0631", expectedResp: "0631", expectedReq: "0621", isResp: true},
		{mti: "0800", expectedResp: "0810", expectedReq: "0800", isResp: false},
		{mti: "0810", expectedResp: "0810", expectedReq: "0800", isResp: true},
		{mti: "0820", expectedResp: "0830", expectedReq: "0820", isResp: false},
	}

	for _, tc := range tests {
		assert.Equal(t, tc.expectedResp, ResponseMTI(tc.mti), "ResponseMTI for %s", tc.mti)
		assert.Equal(t, tc.expectedReq, RequestMTI(tc.mti), "RequestMTI for %s", tc.mti)
		assert.Equal(t, tc.isResp, IsResponseMTI(tc.mti), "IsResponseMTI for %s", tc.mti)
	}
}
