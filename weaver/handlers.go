package main

import (
	"errors"
	"log"
	"net/http"
	"os"
	"runtime"

	"github.com/arachnys/athenapdf/weaver/converter"
	"github.com/arachnys/athenapdf/weaver/converter/athenapdf"
	"github.com/arachnys/athenapdf/weaver/converter/cloudconvert"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/getsentry/raven-go"
	"github.com/gin-gonic/gin"
	"gopkg.in/alexcesaro/statsd.v2"
)

var (
	// ErrURLInvalid should be returned when a conversion URL is invalid.
	ErrURLInvalid = errors.New("invalid URL provided")
	// ErrFileInvalid should be returned when a conversion file is invalid.
	ErrFileInvalid   = errors.New("invalid file provided")
	ErrDomainInvalid = errors.New("invalid domain provided")
	ErrKeyInvalid    = errors.New("invalid Key provided")
)

// indexHandler returns a JSON string indicating that the microservice is online.
// It does not actually check if conversions are working. It is nevertheless,
// used for monitoring.
func indexHandler(c *gin.Context) {
	// We can do better than this...
	c.JSON(http.StatusOK, gin.H{"status": "online"})
}

// statsHandler returns a JSON string containing the number of running
// Goroutines, and pending jobs in the work queue.
func statsHandler(c *gin.Context) {
	q := c.MustGet("queue").(chan<- converter.Work)
	c.JSON(http.StatusOK, gin.H{
		"goroutines": runtime.NumGoroutine(),
		"pending":    len(q),
	})
}

func conversionHandler(c *gin.Context, source converter.ConversionSource) {
	// GC if converting temporary file
	log.Println("转换资源位置 URI: ", source.URI)

	if source.IsLocal {
		defer os.Remove(source.URI)
	}

	_, aggressive := c.GetQuery("aggressive")

	conf := c.MustGet("config").(Config)
	wq := c.MustGet("queue").(chan<- converter.Work)
	s := c.MustGet("statsd").(*statsd.Client)
	r, ravenOk := c.Get("sentry")

	newTiming := s.NewTiming()

	awsConf := converter.AWSS3{
		Region:       c.Query("aws_region"),
		AccessKey:    c.Query("aws_id"),
		AccessSecret: c.Query("aws_secret"),
		S3Bucket:     c.Query("s3_bucket"),
		S3Key:        c.Query("s3_key"),
		S3Acl:        c.Query("s3_acl"),
	}

	log.Printf("待转数量: %d\n", len(wq))
	log.Printf("statsd client 信息: %+v\n", s)
	log.Printf("sentry 信息: %v  %t\n", r, ravenOk)
	log.Printf("AWSS3 配置: %+v\n", awsConf)
	log.Printf("newTiming: %v", newTiming)

	var conversion converter.Converter
	var work converter.Work
	attempts := 0

	baseConversion := converter.Conversion{}
	uploadConversion := converter.UploadConversion{Conversion: baseConversion, AWSS3: awsConf}

StartConversion:
	conversion = athenapdf.AthenaPDF{UploadConversion: uploadConversion, CMD: conf.AthenaCMD, Aggressive: aggressive}
	if attempts != 0 {
		cc := cloudconvert.Client{BaseURL: conf.CloudConvert.APIUrl, APIKey: conf.CloudConvert.APIKey}
		conversion = cloudconvert.CloudConvert{UploadConversion: uploadConversion, Client: cc}
	}
	work = converter.NewWork(wq, conversion, source)

	select {
	case <-c.Writer.CloseNotify():
		work.Cancel()
	case <-work.Uploaded():
		newTiming.Send("conversion_duration")
		s.Increment("success")
		c.JSON(200, gin.H{"status": "uploaded"})
	case out := <-work.AWSS3Success():
		newTiming.Send("AWS S3 conversion_duration")
		s.Increment("success")
		c.Data(200, "application/pdf", out)
	case url := <-work.QiniuSuccess():
		newTiming.Send("Qiniu conversion_duration")
		s.Increment("success")

		urlData := struct{ URL string }{
			URL: url,
		}

		// 有色网前端固定格式 code + msg + data
		c.JSON(200, gin.H{
			"code": 0,
			"msg":  "OK",
			"data": urlData,
		})
	case err := <-work.Error():
		log.Println(err)

		// Log, and stats collection
		if err == converter.ErrConversionTimeout {
			s.Increment("conversion_timeout")
		} else if _, awsError := err.(awserr.Error); awsError {
			s.Increment("s3_upload_error")
			if ravenOk {
				r.(*raven.Client).CaptureError(err, map[string]string{"url": source.GetActualURI()})
			}
		} else {
			s.Increment("conversion_error")
			if ravenOk {
				r.(*raven.Client).CaptureError(err, map[string]string{"url": source.GetActualURI()})
			}
		}

		if attempts == 0 && conf.ConversionFallback {
			s.Increment("cloudconvert")
			log.Println("falling back to CloudConvert...")
			attempts++
			goto StartConversion
		}

		s.Increment("conversion_failed")

		if err == converter.ErrConversionTimeout {
			c.AbortWithError(http.StatusGatewayTimeout, converter.ErrConversionTimeout).SetType(gin.ErrorTypePublic)
			return
		}

		c.Error(err)
	}
}

// convertByURLHandler is the main v1 API handler for converting a HTML to a PDF
// via a GET request. It can either return a JSON string indicating that the
// output of the conversion has been uploaded or it can return the output of
// the conversion to the client (raw bytes).
func convertByURLHandler(c *gin.Context) {
	var token, domain, key string
	s := c.MustGet("statsd").(*statsd.Client)
	r, ravenOk := c.Get("sentry")

	url := c.Query("url")
	ext := c.Query("ext")
	needLogin := c.Query("need_login") // 是否需要登录, 登录需要 token, cookie 之类的东西
	if needLogin == "true" {
		// 组建 token,
		token = c.Query("token")
		if url == "" {
			c.AbortWithError(http.StatusBadRequest, ErrURLInvalid).SetType(gin.ErrorTypePublic)
			s.Increment("invalid_url")
			return
		}

		domain = c.Query("domain")
		if domain == "" {
			c.AbortWithError(http.StatusBadRequest, ErrDomainInvalid).SetType(gin.ErrorTypePublic)
			s.Increment("invalid_domain")
			return
		}

		key = c.Query("key")
		if key == "" {
			c.AbortWithError(http.StatusBadRequest, ErrKeyInvalid).SetType(gin.ErrorTypePublic)
			s.Increment("invalid_key")
			return
		}
	}

	log.Printf("输入参数 url: %s  token: %s  扩展名: %s  域名: %s  TokenKey: %s  needLogin: %s\n", url, token, ext, domain, key, needLogin)

	source, err := converter.NewConversionSource(url, token, key, domain, ext, nil)
	if err != nil {
		s.Increment("conversion_error")
		if ravenOk {
			r.(*raven.Client).CaptureError(err, map[string]string{"url": url})
		}
		c.Error(err)
		return
	}

	conversionHandler(c, *source)
}

func convertByFileHandler(c *gin.Context) {
	s := c.MustGet("statsd").(*statsd.Client)
	r, ravenOk := c.Get("sentry")

	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.AbortWithError(http.StatusBadRequest, ErrFileInvalid).SetType(gin.ErrorTypePublic)
		s.Increment("invalid_file")
		return
	}

	ext := c.Query("ext")

	log.Printf("输入参数 filename: %s  大小: %d  扩展名: %s\n", header.Filename, header.Size, ext)

	source, err := converter.NewConversionSource("", "", "", "", ext, file)
	if err != nil {
		s.Increment("conversion_error")
		if ravenOk {
			r.(*raven.Client).CaptureError(err, map[string]string{"url": header.Filename})
		}
		c.Error(err)
		return
	}

	conversionHandler(c, *source)
}
