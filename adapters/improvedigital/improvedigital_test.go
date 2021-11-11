package improvedigital

import (
	"testing"

	"github.com/prebid/prebid-server/adapters/adapterstest"
	"github.com/prebid/prebid-server/config"
	"github.com/prebid/prebid-server/openrtb_ext"
)

func TestJsonSamples(t *testing.T) {
	bidder, buildErr := Builder(openrtb_ext.BidderImprovedigital, config.Adapter{
		Endpoint: "http://localhost/pbs"})

	if buildErr != nil {
		t.Fatalf("Builder returned unexpected error %v", buildErr)
	}

	adapterstest.RunJSONBidderTest(t, "improvedigitaltest", bidder)
}

func TestValidateAdditionalConsent(t *testing.T) {
	userExt := UserExtReq{
		Consent: "",
		ConsentedProvidersSetting: struct {
			ConsentedProviders string `json:"consented_providers"`
		}{ConsentedProviders: "1~10.20.30"},
	}

	str, err := userExt.hasAdditionalConsent()
	if err != nil {
		t.Fatalf("Additional consent not found")
	}

	expectedValue := "10.20.30"
	if str != expectedValue {
		t.Fatalf("Expected value does not match. expected: %v found:%v", expectedValue, str)
	}
}

func TestParseAdditionalConsent(t *testing.T) {
	userExt := UserExtReq{
		Consent: "",
		ConsentedProvidersSetting: struct {
			ConsentedProviders string `json:"consented_providers"`
		}{ConsentedProviders: "1~10.20.30"},
	}

	addtlConsentStr := "10.20.30"
	expectedValue := []int{10, 20, 30}

	consentProviders := userExt.prepareAdditionalIds(addtlConsentStr)

	if len(consentProviders) != len(expectedValue) {
		t.Fatalf("Expected data length does not match, expected length: %v, found: %v", len(expectedValue), len(consentProviders))
	}

	for i := range expectedValue {
		if expectedValue[i] != consentProviders[i] {
			t.Fatalf("value does not match, expected: %v found: %v", expectedValue[i], consentProviders[i])
		}
	}
}
