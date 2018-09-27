package main

import (
	"fmt"
	"log"
	"os"
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

func uploadGraphs(files []os.FileInfo, bucketName string) {
	sess := session.Must(session.NewSession(&aws.Config{Region: aws.String(region)}))
	uploader := s3manager.NewUploader(sess)
	for _, file := range files {
		filename := imageLocation + "/" + file.Name()
		f, fileReadErr := os.Open(filename)
		if fileReadErr != nil {
			log.Fatal(fmt.Sprintf("failed to open file %q, %v", filename, fileReadErr))
		}

		_, uploadErr := uploader.Upload(&s3manager.UploadInput{
			Bucket: aws.String(bucketName),
			Key:    aws.String(time.Now().Format("2006-01-02") + "-" + file.Name()),
			Body:   f,
		})
		if uploadErr != nil {
			log.Fatal(fmt.Sprintf("failed to upload file, %v", uploadErr))
		}

		fmt.Printf("%v uploaded to s3 \n", filename)
	}
}