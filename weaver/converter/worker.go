package converter

import (
	"errors"
	"log"
	"time"
)

// 超时错误
var (
	ErrConversionTimeout = errors.New("pdf 转换超时")
)

// Worker 工作者信息
type Worker struct {
	id int
}

// InitWorkers 初始化转换队列
// maxWorkers: 最大并行转换数
// maxQueue: 队列里做多等待数量
// timeout: 转换超时时间
func InitWorkers(maxWorkers, maxQueue, timeout int) chan<- Work {
	wq := make(chan Work, maxQueue)

	for i := 0; i < maxWorkers; i++ {
		w := Worker{i}
		go func(wq <-chan Work, w Worker, timeout int) {
			for work := range wq {
				log.Printf("[工号 #%d] 正在转换 pdf 中... (当前等待的转换数量: %d)\n", w.id, len(wq))
				work.Process(timeout)
			}
		}(wq, w, timeout)
	}

	return wq
}

// Work 转换器信息
type Work struct {
	converter Converter
	source    ConversionSource
	out       chan []byte
	url       chan string
	err       chan error
	uploaded  chan struct{}
	done      chan struct{}
}

// NewWork 添加新的转换工作
func NewWork(wq chan<- Work, c Converter, s ConversionSource) Work {
	w := Work{}
	w.converter = c
	w.source = s
	w.out = make(chan []byte, 1)
	w.url = make(chan string, 1)
	w.err = make(chan error, 1)
	w.uploaded = make(chan struct{}, 1)
	w.done = make(chan struct{}, 1)
	go func(wq chan<- Work, w Work) {
		wq <- w
	}(wq, w)
	return w
}

// Process 处理超时信息
func (w Work) Process(timeout int) {
	done := make(chan struct{}, 1)
	defer close(done)

	wout := make(chan []byte, 1)
	werr := make(chan error, 1)
	wurl := make(chan string, 1)

	go func(w Work, done <-chan struct{}, wout chan<- []byte, werr chan<- error) {
		// 转换
		out, err := w.converter.Convert(w.source, done)
		if err != nil {
			werr <- err
			return
		}

		// 七牛上传只需要返回给前端 URL 就好了, 不用自动弹出下载框
		uploaded, url, err := w.converter.UploadQiniu(out)
		if err != nil {
			werr <- err
			return
		}

		if uploaded {
			close(w.uploaded)
			return
		}

		log.Println("七牛返回链接: ", url)

		// 原始返回的是字节数组, 七牛的话, 只返回链接即可
		// wout <- out
		wurl <- url
	}(w, done, wout, werr)

	select {
	case <-w.Cancelled():
	case <-w.Uploaded():
	case out := <-wout:
		w.out <- out
	case url := <-wurl:
		w.url <- url
	case err := <-werr:
		w.err <- err
	case <-time.After(time.Second * time.Duration(timeout)):
		w.err <- ErrConversionTimeout
	}
}

// AWSS3Success returns a channel that will be used for publishing the output of a
// conversion.
func (w Work) AWSS3Success() <-chan []byte {
	return w.out
}

// QiniuSuccess  七牛上传成功
func (w Work) QiniuSuccess() <-chan string {
	return w.url
}

// Error returns a channel that will be used for publishing errors from a
// conversion.
func (w Work) Error() <-chan error {
	return w.err
}

// Uploaded returns a channel that will be closed when a conversion has been
// uploaded.
func (w Work) Uploaded() <-chan struct{} {
	return w.uploaded
}

// Cancel will close the done channel. This will indicate to child Goroutines
// that the job has been terminated, and the results are no longer needed.
func (w Work) Cancel() {
	close(w.done)
}

// Cancelled returns a channel that will indicate when a job has been completed.
func (w Work) Cancelled() <-chan struct{} {
	return w.done
}
