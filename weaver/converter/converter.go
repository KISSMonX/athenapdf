package converter

// Converter 转换器接口
type Converter interface {
	Convert(ConversionSource, <-chan struct{}) ([]byte, error)
	UploadAWSS3([]byte) (bool, error)
	UploadQiniu([]byte) (bool, string, error)
}
