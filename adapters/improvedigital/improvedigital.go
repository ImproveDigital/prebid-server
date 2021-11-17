package improvedigital

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/mxmCherry/openrtb/v15/openrtb2"
	"github.com/prebid/prebid-server/adapters"
	"github.com/prebid/prebid-server/config"
	"github.com/prebid/prebid-server/errortypes"
	"github.com/prebid/prebid-server/openrtb_ext"
)

type ImprovedigitalAdapter struct {
	endpoint string
}

// This struct usage for parse lid from bid response
type LidStruct struct {
	Id      string
	SeatBid []struct {
		Bid []struct {
			DealId     string
			Lid        interface{} `json:"lid"`
			BuyingType interface{} `json:"buying_type"`
		}
	}
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

	lidStruct, lidParseError := prepareLidStruct(response.Body)

	for i := range seatBid.Bid {
		bid := seatBid.Bid[i]

		bidType, err := getMediaTypeForImp(bid.ImpID, internalRequest.Imp)
		if err != nil {
			return nil, []error{err}
		}

		if lidParseError == nil && (lidStruct.SeatBid[0].Bid[i].Lid != "" || lidStruct.SeatBid[0].Bid[i].Lid != nil) {
			handleDealId(lidStruct, &bid, i)
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

func prepareLidStruct(JSONResponse json.RawMessage) (LidStruct, error) {
	var h LidStruct
	err := json.Unmarshal(JSONResponse, &h)

	if err != nil {
		return h, err
	}
	return h, nil
}

func handleDealId(ls LidStruct, bid *openrtb2.Bid, bidIndex int) {
	lbid := ls.SeatBid[0].Bid[bidIndex]
	switch lbid.Lid.(type) {
	case string:
		// When Single Deal ID
		if bid.DealID == "" && lbid.BuyingType != "rtb" {
			bid.DealID = fmt.Sprintf("%v", lbid.Lid)
		}
	case []interface{}:
		// When Multiple Deal ID (rare usage)
		if dealId, err := getOneDealIdFromMultipleDeals(&ls, bidIndex); err == nil {
			bid.DealID = fmt.Sprintf("%v", dealId)
		}
	}
}

func getOneDealIdFromMultipleDeals(ls *LidStruct, bidIndex int) (string, error) {
	buyingTypes := make(map[int]string)
	dealIds := make(map[int]string)

	// Push value from interface to map
	for i, v := range ls.SeatBid[0].Bid[bidIndex].Lid.([]interface{}) {
		dealIds[i] = fmt.Sprintf("%v", v)
	}
	for i, v := range ls.SeatBid[0].Bid[bidIndex].BuyingType.([]interface{}) {
		buyingTypes[i] = fmt.Sprintf("%v", v)
	}

	if len(buyingTypes) != len(dealIds) {
		return "", errors.New("deal and buying type length does not match")
	}

	for i, b := range buyingTypes {
		if b != "rtb" {
			return dealIds[i], nil
		}
	}

	return "", nil
}
