package main

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/s3"
)

func main() {
	svc := s3.New(nil)
	svc.CreateBucket(&s3.CreateBucketInput{
		Bucket: aws.String("my-bucket"),
	})
}
