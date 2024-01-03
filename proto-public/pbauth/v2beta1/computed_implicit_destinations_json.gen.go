// Code generated by protoc-json-shim. DO NOT EDIT.
package authv2beta1

import (
	protojson "google.golang.org/protobuf/encoding/protojson"
)

// MarshalJSON is a custom marshaler for ComputedImplicitDestinations
func (this *ComputedImplicitDestinations) MarshalJSON() ([]byte, error) {
	str, err := ComputedImplicitDestinationsMarshaler.Marshal(this)
	return []byte(str), err
}

// UnmarshalJSON is a custom unmarshaler for ComputedImplicitDestinations
func (this *ComputedImplicitDestinations) UnmarshalJSON(b []byte) error {
	return ComputedImplicitDestinationsUnmarshaler.Unmarshal(b, this)
}

// MarshalJSON is a custom marshaler for ImplicitDestination
func (this *ImplicitDestination) MarshalJSON() ([]byte, error) {
	str, err := ComputedImplicitDestinationsMarshaler.Marshal(this)
	return []byte(str), err
}

// UnmarshalJSON is a custom unmarshaler for ImplicitDestination
func (this *ImplicitDestination) UnmarshalJSON(b []byte) error {
	return ComputedImplicitDestinationsUnmarshaler.Unmarshal(b, this)
}

var (
	ComputedImplicitDestinationsMarshaler   = &protojson.MarshalOptions{}
	ComputedImplicitDestinationsUnmarshaler = &protojson.UnmarshalOptions{DiscardUnknown: false}
)