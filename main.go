package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/service/autoscaling"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
)

var debug bool
var showHistogram bool
var timeRange = 7 * 24 * 60 * time.Minute

var region = "ap-southeast-2"
var imageLocation = "images"

const imageFormat = "png"

func (a xyz) Len() int                    { return len(a.x) }
func (a xyz) XY(i int) (x, y float64)     { return a.x[i], a.y[i] }
func (a xyz) XYZ(i int) (x, y, z float64) { return a.x[i], a.y[i], a.z[i] }

func main() {

	flag.BoolVar(&debug, "d", false, "debug")
	flag.BoolVar(&showHistogram, "histogram", false, "show cpu histogram")
	flag.StringVar(&region, "region", "ap-southeast-2", "AWS region")
	flag.StringVar(&imageLocation, "image-location", imageLocation, "Directory to output graphs to must exist and be writeable")
	flag.Parse()

	webGroups := getWebGroups()

	var wg sync.WaitGroup
	wg.Add(len(webGroups))
	for _, webGroup := range webGroups {
		go func(webGroup asg) {
			defer wg.Done()
			doStack(imageLocation+"/"+webGroup.Name+"."+imageFormat, webGroup.Name, webGroup.AsgName, webGroup.RdsName)

		}(webGroup)
	}

	wg.Wait()

	sendImagesToS3()
}

func sendImagesToS3() {
	bucketName := "mrhandy-graphs"
	exists := checkBucketExists(bucketName)

	if exists == false {
		createBucket(bucketName)
	}

	files, ListFilesErr := ioutil.ReadDir(imageLocation)
	if ListFilesErr != nil {
		log.Fatal(ListFilesErr)
	}

	uploadGraphs(files, bucketName)
}

func getWebGroups() []asg {
	var groups []string
	var err error
	if groups, err = autoScalingGroups(region); err != nil {
		log.Fatal(err)
	}

	var webGroups []asg
	for _, asgName := range groups {
		if !strings.Contains(asgName, "WebServerGroup") {
			continue
		}
		parts := strings.Split(asgName, "-")
		if len(parts) <= 3 {
			continue
		}
		name := fmt.Sprintf("%s.%s.%s.web", parts[0], parts[1], parts[2])
		rdsName := fmt.Sprintf("%s-%s-%s", parts[0], parts[1], parts[2])
		asg := asg{
			Name:    name,
			AsgName: asgName,
			RdsName: rdsName,
		}

		webGroups = append(webGroups, asg)
	}

	return webGroups
}

func autoScalingGroups(reg string) ([]string, error) {
	sess := session.Must(session.NewSession(&aws.Config{Region: aws.String(reg)}))
	client := autoscaling.New(sess)
	var result []string
	input := &autoscaling.DescribeAutoScalingGroupsInput{}
	fnc := func(res *autoscaling.DescribeAutoScalingGroupsOutput, lastPage bool) bool {
		for _, g := range res.AutoScalingGroups {
			result = append(result, *g.AutoScalingGroupName)
		}
		return true
	}
	return result, client.DescribeAutoScalingGroupsPages(input, fnc)
}

func doStack(imageName, stackName, asgName, rdsName string) {

	period := NewPeriod(timeRange)

	requests, err := getRequestMetric(stackName, period)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get request metrics for '%s': %v \n", stackName, err)
		return
	}

	cpuCores, err := getCPUCoreMetric(stackName, period)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get CPU core metrics for '%s': %v \n", stackName, err)
		return
	}

	cpuUtil, err := getASGCpuUtilization(asgName, period)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get CPU utilisation metrics for '%s': %v\n", stackName, err)
		return
	}

	rdsUtil, err := getRDSCpuUtilization(rdsName, period)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get RDS CPU utilisation metrics for '%s': %v\n", stackName, err)
		return
	}

	f, p, err := createPlot(imageName, stackName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create plot for '%s': %v\n", stackName, err)
		return
	}

	if showHistogram {
		if err = histogram(p, cpuUtil); err != nil {
			fmt.Fprintf(os.Stderr, "could not plot data for '%s': %v\n", stackName, err)
			return
		}
	} else {
		cpuMean, err := plotScatter(p, requests, cpuUtil, cpuCores, rdsUtil)
		if err != nil {
			fmt.Fprintf(os.Stderr, "could not plot data for '%s': %v\n", stackName, err)
			return
		}

		if cpuMean > 70 {
			fmt.Printf("%s is a candidate for upscaling, mean CPU: %0.1f%%\n", stackName, cpuMean)
		} else if cpuMean < 30 {
			fmt.Printf("%s is a candidate for downscaling, mean CPU: %0.1f%%\n", stackName, cpuMean)
		}
	}

	// w/h - A4 (1:1.414)
	if err := writePlot(f, p, 1024, 1024*(1/1.414)); err != nil {
		fmt.Fprintf(os.Stderr, "Could not write out plot %v\n", err)
		return
	}

}
