package main

import (
	"fmt"
	"math"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/cloudwatch"
)

const Day = time.Hour * 24

func NewPeriod(span time.Duration) *Period {
	return &Period{
		span:   span,
		Period: 60,
	}
}

type Period struct {
	span   time.Duration
	Period int64
}

func (period *Period) Days() float64 {
	return float64(period.span) / float64(Day)
}

func (period *Period) Start(days float64) time.Time {
	return time.Now().Add(time.Duration(float64(Day)*-days) - time.Second)
}

func (period *Period) End(days float64) time.Time {
	days = math.Max(0, days-1)
	return time.Now().Add(time.Duration(float64(Day) * -days))
}

func getRequestMetric(name string, period *Period) (map[int]float64, error) {
	sess := session.Must(session.NewSession(&aws.Config{Region: aws.String(region)}))
	client := cloudwatch.New(sess)

	result := make(map[int]float64, 0)

	const requestMetric = "apache.requests"

	for i := period.Days(); i > 0; i-- {

		res, err := client.GetMetricStatistics(&cloudwatch.GetMetricStatisticsInput{
			Dimensions: []*cloudwatch.Dimension{
				{
					Name:  aws.String("Stack"),
					Value: aws.String(name),
				},
			},
			Namespace:  aws.String("SS"),
			MetricName: aws.String(requestMetric),
			StartTime:  aws.Time(period.Start(i)),
			EndTime:    aws.Time(period.End(i)),
			Period:     aws.Int64(period.Period),
			Statistics: []*string{aws.String("Sum")},
		})

		if err != nil {
			fmt.Println(err)
			return nil, err
		}

		sumMetric(res.Datapoints, result)
	}

	if len(result) == 0 {
		return nil, fmt.Errorf("no datapoints were found for '%s'", requestMetric)
	}

	return result, nil
}

func getCPUCoreMetric(name string, period *Period) (map[int]float64, error) {
	sess := session.Must(session.NewSession(&aws.Config{Region: aws.String(region)}))
	client := cloudwatch.New(sess)

	result := make(map[int]float64, 0)

	const cpuCoreMetric = "cpu.count"

	for i := period.Days(); i > 0; i-- {

		res, err := client.GetMetricStatistics(&cloudwatch.GetMetricStatisticsInput{
			Dimensions: []*cloudwatch.Dimension{
				{
					Name:  aws.String("Stack"),
					Value: aws.String(name),
				},
			},
			Namespace:  aws.String("SS"),
			MetricName: aws.String(cpuCoreMetric),
			StartTime:  aws.Time(period.Start(i)),
			EndTime:    aws.Time(period.End(i)),
			Period:     aws.Int64(period.Period),
			Statistics: []*string{aws.String("Sum")},
		})

		if err != nil {
			fmt.Println(err)
			return nil, err
		}

		sumMetric(res.Datapoints, result)
	}

	if len(result) == 0 {
		return nil, fmt.Errorf("no datapoints were found for '%s'", cpuCoreMetric)
	}

	return result, nil
}

func getASGCpuUtilization(asgName string, period *Period) (map[int]float64, error) {
	sess := session.Must(session.NewSession(&aws.Config{Region: aws.String(region)}))
	client := cloudwatch.New(sess)

	result := make(map[int]float64, 0)

	const metricName = "CPUUtilization"
	for i := period.Days(); i > 0; i-- {

		res, err := client.GetMetricStatistics(&cloudwatch.GetMetricStatisticsInput{
			Dimensions: []*cloudwatch.Dimension{
				{
					Name:  aws.String("AutoScalingGroupName"),
					Value: aws.String(asgName),
				},
			},
			Namespace:  aws.String("AWS/EC2"),
			MetricName: aws.String(metricName),
			StartTime:  aws.Time(period.Start(i)),
			EndTime:    aws.Time(period.End(i)),
			Period:     aws.Int64(period.Period),
			Statistics: []*string{aws.String("Sum")},
		})

		if err != nil {
			return nil, err
		}

		if len(res.Datapoints) == 0 {
			return nil, fmt.Errorf("no datapoints were found for %s", *res.Label)
		}

		sumMetric(res.Datapoints, result)
	}
	return result, nil
}

func getRDSCpuUtilization(stackName string, period *Period) (map[int]float64, error) {
	sess := session.Must(session.NewSession(&aws.Config{Region: aws.String(region)}))
	client := cloudwatch.New(sess)

	result := make(map[int]float64, 0)

	const metricName = "CPUUtilization"
	for i := period.Days(); i > 0; i-- {

		res, err := client.GetMetricStatistics(&cloudwatch.GetMetricStatisticsInput{
			Dimensions: []*cloudwatch.Dimension{
				{
					Name:  aws.String("DBInstanceIdentifier"),
					Value: aws.String(stackName),
				},
			},
			Namespace:  aws.String("AWS/RDS"),
			MetricName: aws.String(metricName),
			StartTime:  aws.Time(period.Start(i)),
			EndTime:    aws.Time(period.End(i)),
			Period:     aws.Int64(period.Period),
			Statistics: []*string{aws.String("Sum")},
		})

		if err != nil {
			return nil, err
		}

		if len(res.Datapoints) == 0 {
			return nil, fmt.Errorf("no datapoints were found for %s", *res.Label)
		}

		sumMetric(res.Datapoints, result)
	}
	return result, nil
}

func sumMetric(in []*cloudwatch.Datapoint, result map[int]float64) {
	for _, point := range in {
		ago := minutesAgo(point)
		// if value already exists, add it to current (Sum metric)
		if v, found := result[ago]; found {
			if found {
				v += *point.Sum
			}
		} else {
			result[ago] = *point.Sum
		}
	}
}

func minutesAgo(point *cloudwatch.Datapoint) int {
	since := time.Since(*point.Timestamp)
	return int(math.Floor(float64(since / time.Minute)))
}
