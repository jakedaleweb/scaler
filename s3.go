package main

import (
	"fmt"
	"io"
	"log"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
)

func checkBucketExists(name string) bool {
	sess := session.Must(session.NewSession(&aws.Config{Region: aws.String(region)}))
	svc := s3.New(sess)

	list, _ := svc.ListBuckets(nil)

	for _, bucket := range list.Buckets {
		if *bucket.Name == name {
			return true
		}
	}

	return false
}

func createBucket(name string) {
	exists := checkBucketExists(name)
	if exists == true {
		return
	}

	sess := session.Must(session.NewSession(&aws.Config{Region: aws.String(region)}))
	svc := s3.New(sess)

	_, bucketCreateErr := svc.CreateBucket(&s3.CreateBucketInput{
		Bucket: aws.String(name),
	})
	if bucketCreateErr != nil {
		log.Fatal(fmt.Sprintf("Unable to create bucket %q, %v", name, bucketCreateErr))
	}

	fmt.Printf("Waiting for bucket %q to be created...\n", name)
	waitErr := svc.WaitUntilBucketExists(&s3.HeadBucketInput{
		Bucket: aws.String(name),
	})
	if waitErr != nil {
		log.Fatal(fmt.Sprintf("Error occurred while waiting for bucket to be created, %v", name))
	}

	fmt.Printf("Bucket %q successfully created\n", name)
}

func pipeToS3(pipe *io.PipeReader, stackName string, bucketName string) string {
	sess := session.Must(session.NewSession(&aws.Config{Region: aws.String(region)}))
	uploader := s3manager.NewUploader(sess)

	location := time.Now().Format("2006-01-02") + " " + stackName + ".png"
	_, err := uploader.Upload(&s3manager.UploadInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(location),
		Body:   pipe,
	})

	if err != nil {
		log.Fatal(fmt.Sprintf("Error occurred while piping graph to s3 for %v", stackName))
	}

	link := getPresignedLink(location, bucketName)
	return link
}

func getPresignedLink(location string, bucketName string) string {
	sess := session.Must(session.NewSession(&aws.Config{Region: aws.String(region)}))
	svc := s3.New(sess)

	req, _ := svc.GetObjectRequest(&s3.GetObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(location),
	})
	urlStr, err := req.Presign(1 * time.Hour)

	if err != nil {
		log.Fatal("Failed to sign request", err)
	}

	return urlStr
}
