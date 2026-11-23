//go:build with_utls

package ewp

import "testing"

func TestEWPOrdinaryXHTTPIndependentDownloadSafari(t *testing.T) {
	testEWPXHTTPIndependentDownload(t, "safari")
}
