package converter

import (
	"reflect"
	"testing"
)

func TestConversion_Convert(t *testing.T) {
	mockConversion := Conversion{}
	mockSource := ConversionSource{}
	mockDone := make(chan struct{}, 1)
	got, err := mockConversion.Convert(mockSource, mockDone)
	if err != nil {
		t.Fatalf("convert returned an unexpected error: %+v", err)
	}
	if want := []byte{}; !reflect.DeepEqual(got, want) {
		t.Errorf("expected output of conversion to be %+v, got %+v", want, got)
	}
}

func TestConversion_UploadAWSS3(t *testing.T) {
	mockConversion := Conversion{}
	got, err := mockConversion.UploadAWSS3(nil)
	if err != nil {
		t.Fatalf("upload AWS S3 returned an unexpected error: %+v", err)
	}
	if want := false; got != want {
		t.Errorf("expected status of upload to be %+v, got %+v", want, got)
	}
}

func TestConversion_UploadQiniu(t *testing.T) {
	mockConversion := Conversion{}
	got, url, err := mockConversion.UploadQiniu(nil)
	if err != nil {
		t.Fatalf("upload Qiniut returned an unexpected error: %+v", err)
	}
	if want := false; got != want {
		t.Errorf("expected status of upload to be %+v, got %+v  %+v", want, got, url)
	}
}
