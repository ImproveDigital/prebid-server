package improvedigital

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/mxmCherry/openrtb/v15/openrtb2"
	"github.com/prebid/prebid-server/adapters"
	"github.com/prebid/prebid-server/config"
	"github.com/prebid/prebid-server/errortypes"
	"github.com/prebid/prebid-server/openrtb_ext"
)

type ImprovedigitalAdapter struct {
	endpoint string
}

type UserExtReq struct {
	Consent                   string                     `json:"consent,omitempty"`
	Prebid                    *openrtb_ext.ExtUserPrebid `json:"prebid,omitempty"`
	Eids                      []openrtb_ext.ExtUserEid   `json:"eids,omitempty"`
	ConsentedProvidersSetting struct {
		ConsentedProviders string `json:"consented_providers"`
	} `json:"ConsentedProvidersSettings,omitempty"`
}

type UserExtRes struct {
	Consent                   string                     `json:"consent,omitempty"`
	Prebid                    *openrtb_ext.ExtUserPrebid `json:"prebid,omitempty"`
	Eids                      []openrtb_ext.ExtUserEid   `json:"eids,omitempty"`
	ConsentedProvidersSetting struct {
		ConsentedProviders []int `json:"consented_providers,omitempty"`
	} `json:"consented_providers_settings,omitempty"`
}

// MakeRequests makes the HTTP requests which should be made to fetch bids.
func (a *ImprovedigitalAdapter) MakeRequests(request *openrtb2.BidRequest, reqInfo *adapters.ExtraRequestInfo) ([]*adapters.RequestData, []error) {
	numRequests := len(request.Imp)
	errors := make([]error, 0)
	adapterRequests := make([]*adapters.RequestData, 0, numRequests)

	// Split multi-imp request into multiple ad server requests. SRA is currently not recommended.
	for i := 0; i < numRequests; i++ {
		if adapterReq, err := a.makeRequest(*request, request.Imp[i]); err == nil {
			adapterRequests = append(adapterRequests, adapterReq)
		} else {
			errors = append(errors, err)
		}
	}

	return adapterRequests, errors
}

func (a *ImprovedigitalAdapter) makeRequest(request openrtb2.BidRequest, imp openrtb2.Imp) (*adapters.RequestData, error) {
	request.Imp = []openrtb2.Imp{imp}

	reqJSON, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}

	if reqCloneJSON, err := a.getJSONWithAdditionalConsent(request, reqJSON); err == nil {
		reqJSON = reqCloneJSON
	}

	headers := http.Header{}
	headers.Add("Content-Type", "application/json;charset=utf-8")

	return &adapters.RequestData{
		Method:  "POST",
		Uri:     a.endpoint,
		Body:    reqJSON,
		Headers: headers,
	}, nil
}

// MakeBids unpacks the server's response into Bids.
func (a *ImprovedigitalAdapter) MakeBids(internalRequest *openrtb2.BidRequest, externalRequest *adapters.RequestData, response *adapters.ResponseData) (*adapters.BidderResponse, []error) {
	if response.StatusCode == http.StatusNoContent {
		return nil, nil
	}

	if response.StatusCode == http.StatusBadRequest {
		return nil, []error{&errortypes.BadInput{
			Message: fmt.Sprintf("Unexpected status code: %d. Run with request.debug = 1 for more info", response.StatusCode),
		}}
	}

	if response.StatusCode != http.StatusOK {
		return nil, []error{&errortypes.BadServerResponse{
			Message: fmt.Sprintf("Unexpected status code: %d. Run with request.debug = 1 for more info", response.StatusCode),
		}}
	}

	var bidResp openrtb2.BidResponse
	if err := json.Unmarshal(response.Body, &bidResp); err != nil {
		return nil, []error{err}
	}

	if len(bidResp.SeatBid) == 0 {
		return nil, nil
	}

	if len(bidResp.SeatBid) > 1 {
		return nil, []error{&errortypes.BadServerResponse{
			Message: fmt.Sprintf("Unexpected SeatBid! Must be only one but have: %d", len(bidResp.SeatBid)),
		}}
	}

	seatBid := bidResp.SeatBid[0]

	if len(seatBid.Bid) == 0 {
		return nil, nil
	}

	bidResponse := adapters.NewBidderResponseWithBidsCapacity(len(seatBid.Bid))

	for i := range seatBid.Bid {
		bid := seatBid.Bid[i]

		bidType, err := getMediaTypeForImp(bid.ImpID, internalRequest.Imp)
		if err != nil {
			return nil, []error{err}
		}

		bidResponse.Bids = append(bidResponse.Bids, &adapters.TypedBid{
			Bid:     &bid,
			BidType: bidType,
		})
	}
	return bidResponse, nil

}

// Builder builds a new instance of the Improvedigital adapter for the given bidder with the given config.
func Builder(bidderName openrtb_ext.BidderName, config config.Adapter) (adapters.Bidder, error) {
	bidder := &ImprovedigitalAdapter{
		endpoint: config.Endpoint,
	}
	return bidder, nil
}

func getMediaTypeForImp(impID string, imps []openrtb2.Imp) (openrtb_ext.BidType, error) {
	for _, imp := range imps {
		if imp.ID == impID {
			if imp.Banner != nil {
				return openrtb_ext.BidTypeBanner, nil
			}

			if imp.Video != nil {
				return openrtb_ext.BidTypeVideo, nil
			}

			if imp.Native != nil {
				return openrtb_ext.BidTypeNative, nil
			}

			return "", &errortypes.BadServerResponse{
				Message: fmt.Sprintf("Unknown impression type for ID: \"%s\"", impID),
			}
		}
	}

	// This shouldnt happen. Lets handle it just incase by returning an error.
	return "", &errortypes.BadServerResponse{
		Message: fmt.Sprintf("Failed to find impression for ID: \"%s\"", impID),
	}
}

func (a *ImprovedigitalAdapter) getJSONWithAdditionalConsent(request openrtb2.BidRequest, reqJSON json.RawMessage) (json.RawMessage, error) {
	var userExtReq UserExtReq

	// If user not defined, no need to parse additional consent
	if request.User == nil {
		return nil, errors.New("")
	}

	// Clone request due to testMakeRequestsImpl don't want the adapters' implementation of `MakeRequests()` to modify
	// Ref: adapters/adapterstest/test_json.go
	var reqClone openrtb2.BidRequest
	if err := json.Unmarshal(reqJSON, &reqClone); err != nil {
		return nil, err
	}

	if err := json.Unmarshal(reqClone.User.Ext, &userExtReq); err != nil {
		return nil, errors.New("")
	}

	if str, err := userExtReq.hasAdditionalConsent(); err == nil {

		userExtRes := UserExtRes{
			Consent: userExtReq.Consent,
			Prebid:  userExtReq.Prebid,
			Eids:    userExtReq.Eids,
		}
		userExtRes.ConsentedProvidersSetting.ConsentedProviders = userExtReq.prepareAdditionalIds(str)

		extJson, extErr := json.Marshal(userExtRes)
		if extErr != nil {
			return nil, errors.New("unable to parse user.ext")
		}

		reqClone.User.Ext = extJson
		reqCloneJSON, reqErr := json.Marshal(reqClone)
		if reqErr == nil {
			return reqCloneJSON, reqErr
		}
	}

	return nil, errors.New("additional consent not found")
}

func (ue UserExtReq) hasAdditionalConsent() (string, error) {
	tildaPosition := strings.Index(ue.ConsentedProvidersSetting.ConsentedProviders, "~")
	if tildaPosition != -1 {
		atpIdsString := ue.ConsentedProvidersSetting.ConsentedProviders[tildaPosition+1:]

		return atpIdsString, nil
	}

	return "", errors.New("no additional consent found")
}

func (UserExtReq) prepareAdditionalIds(str string) []int {
	additionalIds := strings.Split(str, ".")
	atpIdsInt := make([]int, 0) // may be we can use max length of additionalIds
	for _, id := range additionalIds {
		if i, err := strconv.Atoi(id); err == nil {
			atpIdsInt = append(atpIdsInt, i)
		}
	}

	return atpIdsInt
}
