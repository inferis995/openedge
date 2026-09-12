package models

// TagPayload is the JSON body of every data/{org}/{site}/{area}/{gateway}/{alias}
// message. It is the contract between the drivers that publish and the three
// services that consume — core-api, engine-historian and the cloud bridge —
// and it was previously declared eight times, once in each of them.
//
// One declaration matters more here than in most places: a consumer whose copy
// is missing a field does not fail to parse, it reads the zero value. A
// missing EUScaled reads as false, which is precisely the state that causes a
// second conversion of an already-converted value.
type TagPayload struct {
	TagID     int         `json:"tag_id"`
	OrgID     int         `json:"org_id"`
	Value     interface{} `json:"v"`
	Timestamp int64       `json:"ts"`
	Quality   int         `json:"q"`

	// EUScaled says the publisher has already applied the tag's
	// engineering-unit conversion, so no one downstream should apply it again.
	//
	// It exists because drivers and core-api are updated independently: the
	// driver containers come down through OTA, core-api with the stack. During
	// the window where one is new and the other is not, this flag is the only
	// thing distinguishing a value in bar from the same number in raw counts.
	// Both readings are plausible; neither raises an error.
	//
	// Omitted when false so an old consumer sees exactly the payload it used
	// to see.
	EUScaled bool `json:"eu,omitempty"`
}
