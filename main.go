package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net/smtp"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/subosito/gotenv"

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
	gotenv.Load()

	flag.BoolVar(&debug, "d", false, "debug")
	flag.BoolVar(&showHistogram, "histogram", false, "show cpu histogram")
	flag.StringVar(&region, "region", "ap-southeast-2", "AWS region")
	flag.Parse()

	webGroups := getWebGroups()

	var wg sync.WaitGroup
	wg.Add(len(webGroups))
	for _, webGroup := range webGroups {
		go func(webGroup asg) {
			defer wg.Done()
			doStack(webGroup.Name, webGroup.AsgName, webGroup.RdsName)

		}(webGroup)
	}

	wg.Wait()
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

func doStack(stackName, asgName, rdsName string) {

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

	p, err := createPlot(stackName)
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

		message := ""
		if cpuMean > 70 {
			message = fmt.Sprintf("%s is a candidate for upscaling, mean CPU: %0.1f%%\n", stackName, cpuMean)
		} else if cpuMean < 30 {
			message = fmt.Sprintf("%s is a candidate for downscaling, mean CPU: %0.1f%%\n", stackName, cpuMean)
		}

		if message != "" {
			emailTeam(message)
		}
	}

	rPipe, wPipe := io.Pipe()
	go func() {
		writePlot(wPipe, p, 1024, 1024*(1/1.414))
	}()
	pipeToS3(rPipe, stackName)
}

func emailTeam(message string) {
	from := os.Getenv("EMAIL_USER")
	pass := os.Getenv("EMAIL_PASSWORD")
	to := os.Getenv("EMAIL_RECIPIENT")

	msg := "From: " + from + "\n" +
		"To: " + to + "\n" +
		"Subject: Mr Handy notification 🤖 \n\n" +
		message

	err := smtp.SendMail("smtp.gmail.com:587",
		smtp.PlainAuth("", from, pass, "smtp.gmail.com"),
		from, []string{to}, []byte(msg))

	if err != nil {
		log.Printf("smtp error: %s", err)
		return
	}
}
