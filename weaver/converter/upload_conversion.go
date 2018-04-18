package converter

import (
	"bytes"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awsutil"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/smmit/smmbase/logger"
	"github.com/smmit/smmbase/qiniu"
)

// AWSS3 AWS S3配置
type AWSS3 struct {
	Region       string
	AccessKey    string
	AccessSecret string
	S3Bucket     string
	S3Key        string
	S3Acl        string
}

// UploadConversion 转换器
type UploadConversion struct {
	Conversion
	AWSS3
}

// uploadToS3 上传到 AWS S3
func uploadToS3(awsConf AWSS3, b []byte) error {
	logger.Debugf("[Converter] uploading conversion to S3 bucket '%s' with key '%s'\n", awsConf.S3Bucket, awsConf.S3Key)
	st := time.Now()

	region := "us-east-1"
	if awsConf.Region != "" {
		region = awsConf.Region
	}

	acl := "public-read"
	if awsConf.S3Acl != "" {
		acl = awsConf.S3Acl
	}

	conf := aws.NewConfig().WithRegion(region).WithMaxRetries(3)

	if awsConf.AccessKey != "" && awsConf.AccessSecret != "" {
		creds := credentials.NewStaticCredentials(awsConf.AccessKey, awsConf.AccessSecret, "")

		// Credential 'Value'
		_, err := creds.Get()
		if err != nil {
			return err
		}

		conf = conf.WithCredentials(creds)
	}

	sess := session.New(conf)
	svc := s3.New(sess)

	p := &s3.PutObjectInput{
		Bucket:      aws.String(awsConf.S3Bucket),
		Key:         aws.String(awsConf.S3Key),
		ACL:         aws.String(acl),
		ContentType: aws.String("application/pdf"),
		Body:        bytes.NewReader(b),
	}

	res, err := svc.PutObject(p)
	if err != nil {
		return err
	}

	et := time.Now()
	logger.Debugf("[Converter] uploaded to S3: %s (%s)\n", awsutil.StringValue(res), et.Sub(st))
	return nil
}

// UploadAWSS3 UploadConversion 上传方法
func (c UploadConversion) UploadAWSS3(b []byte) (bool, error) {
	if c.AWSS3.S3Bucket == "" || c.AWSS3.S3Key == "" {
		return false, nil
	}

	if err := uploadToS3(c.AWSS3, b); err != nil {
		return false, err
	}

	return true, nil
}

// UploadQiniu 上传到七牛
func (c UploadConversion) UploadQiniu(b []byte) (bool, string, error) {
	st := time.Now()

	qn := qiniu.NewSmmQiniuUpload()

	finalkey, err := qn.QiniuUploadFile(qiniu.CatMap[qiniu.TRADECENTER_CREDENTIAL_FILE], b, ".pdf")
	if err != nil {
		logger.Warnning("上传七牛失败: ", err.Error())
		return false, "", err
	}
	et := time.Now()
	logger.Debugf("上传七牛耗时: %s\n", et.Sub(st))
	return true, finalkey, nil
}
