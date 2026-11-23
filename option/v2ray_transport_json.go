package option

import (
	"context"
	"reflect"

	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/schema"
	"github.com/sagernet/sing/common/json"
	"github.com/sagernet/sing/common/json/badjson"
)

func hasV2RayTransport(protocol string) bool {
	switch protocol {
	case C.TypeVLESS, C.TypeVMess, C.TypeTrojan, C.TypeEWP:
		return true
	}
	return false
}

// badjson.JSONObject drops empty nested objects when it is re-encoded.
// XHTTP uses an empty object to override an inherited headers or xmux object.
// Keep the nested values opaque when merging or splitting the type/tag fields.
type rawOptionsObject = badjson.TypedMap[string, json.RawMessage]

func marshalRawOptions(ctx context.Context, base, options any) ([]byte, error) {
	baseJSON, err := json.MarshalContext(ctx, base)
	if err != nil || options == nil {
		return baseJSON, err
	}
	optionsJSON, err := json.MarshalContext(ctx, options)
	if err != nil {
		return nil, err
	}
	var object, fields rawOptionsObject
	if err = json.UnmarshalContext(ctx, baseJSON, &object); err != nil {
		return nil, err
	}
	if err = json.UnmarshalContext(ctx, optionsJSON, &fields); err != nil {
		return nil, err
	}
	object.PutAll(&fields)
	return json.MarshalContext(ctx, &object)
}

func unmarshalRawOptions(ctx context.Context, data []byte, options any, exclude ...string) error {
	var object rawOptionsObject
	if err := json.UnmarshalContext(ctx, data, &object); err != nil {
		return err
	}
	for _, key := range exclude {
		object.Remove(key)
	}
	data, err := json.MarshalContext(ctx, &object)
	if err != nil {
		return err
	}
	return json.UnmarshalContextDisallowUnknownFields(ctx, data, options)
}

func (o V2RayXHTTPDownloadOptions) DescribeSchema(builder schema.Builder) (*schema.Node, error) {
	return builder.Define("V2RayXHTTPDownloadOptions", func() (*schema.Node, error) {
		object := schema.StrictObject()
		return object, builder.FlattenStruct(object, reflect.TypeFor[V2RayXHTTPDownloadOptions]())
	})
}

func (o *V2RayXHTTPDownloadOptions) UnmarshalJSON(data []byte) error {
	// Older sing-box versions embedded the entire upload options here. They
	// accepted these fields but did not apply them to the GET. Keep accepting
	// them so previously formatted or saved configs still load.
	type downloadOptions V2RayXHTTPDownloadOptions
	var download downloadOptions
	legacy := struct {
		*downloadOptions
		Mode                 *string          `json:"mode"`
		NoGRPCHeader         bool             `json:"no_grpc_header"`
		NoSSEHeader          bool             `json:"no_sse_header"`
		GRPCFraming          bool             `json:"grpc_framing"`
		ScMaxEachPostBytes   *V2RayXHTTPRange `json:"sc_max_each_post_bytes"`
		ScMinPostsIntervalMs *V2RayXHTTPRange `json:"sc_min_posts_interval_ms"`
		ScMaxBufferedPosts   int64            `json:"sc_max_buffered_posts"`
		ScStreamUpServerSecs *V2RayXHTTPRange `json:"sc_stream_up_server_secs"`
	}{downloadOptions: &download}
	if err := json.UnmarshalDisallowUnknownFields(data, &legacy); err != nil {
		return err
	}
	if legacy.Mode != nil && legacy.ScMaxEachPostBytes != nil && legacy.ScMinPostsIntervalMs != nil && legacy.ScStreamUpServerSecs != nil &&
		download.XPaddingBytes != nil && *download.XPaddingBytes == (V2RayXHTTPRange{}) {
		// Older formatting always wrote all these fields, including the
		// unused x_padding_bytes: "0-0". Do not turn that into a reset.
		download.XPaddingBytes = nil
	}
	*o = V2RayXHTTPDownloadOptions(download)
	return nil
}
