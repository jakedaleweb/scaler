package main

import (
	"flag"
	"fmt"
	"image/color"
	"log"
	"os"
	"strings"
	"time"

	"gonum.org/v1/gonum/stat"

	"github.com/aws/aws-sdk-go/service/autoscaling"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"

	"gonum.org/v1/plot"
	"gonum.org/v1/plot/plotter"
	"gonum.org/v1/plot/vg"
	"gonum.org/v1/plot/vg/draw"
)

var debug bool
var showHistogram bool
var timeRange = 7 * 24 * 60 * time.Minute

var region = "ap-southeast-2"

const imageFormat = "png"

type xyz struct{ x, y, z []float64 }

func (a xyz) Len() int                    { return len(a.x) }
func (a xyz) XY(i int) (x, y float64)     { return a.x[i], a.y[i] }
func (a xyz) XYZ(i int) (x, y, z float64) { return a.x[i], a.y[i], a.z[i] }

func main() {

	flag.BoolVar(&debug, "d", false, "debug")
	flag.BoolVar(&showHistogram, "histogram", false, "show cpu histogram")
	flag.StringVar(&region, "region", "ap-southeast-2", "AWS region")
	flag.Parse()

	var groups []string
	var err error
	if groups, err = autoScalingGroups(region); err != nil {
		log.Fatal(err)
	}
	//groups = []string{
	//	"sssites-ssorg-prod-WebServerGroup-M4B333CXXJQU",
	//	"apollo-bcdgroup-prod-WebServerGroup-GAP7WJ4R2ZRT",
	//}

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
		doStack(name+"."+imageFormat, name, asgName, rdsName)
	}
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
		//fmt.Fprintf(os.Stderr, "Failed to get request metrics for '%s': %v \n", stackName, err)
		return
	}

	cpuCores, err := getCPUCoreMetric(stackName, period)
	if err != nil {
		//fmt.Fprintf(os.Stderr, "Failed to get CPU core metrics for '%s': %v \n", stackName, err)
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

func plotScatter(p *plot.Plot, requests, cpuUtil, cpuCores, rdsUtil map[int]float64) (float64, error) {
	p.X.Label.Text = "reqs / min"
	p.Y.Label.Text = "t2 usage %"
	p.Y.Min = 0
	p.Y.Max = 101

	ec2Data := &xyz{}
	rdsData := &xyz{}
	for ago, cpuUtilization := range cpuUtil {
		cores, found := cpuCores[ago]
		if !found {
			continue
		}
		request, found := requests[ago]
		if !found {
			request = 0
		}

		rdsUtilization, found := rdsUtil[ago]
		if !found {
			rdsUtilization = 0
		}

		ec2Data.x = append(ec2Data.x, request)
		ec2Data.y = append(ec2Data.y, cpuUtilization/cores)
		ec2Data.z = append(ec2Data.z, cores)

		rdsData.x = append(rdsData.x, request)
		rdsData.y = append(rdsData.y, rdsUtilization)
	}

	rdsScatter, err := plotter.NewScatter(rdsData)
	if err != nil {
		return 0, fmt.Errorf("could not create scatter plot: %v", err)
	}
	rdsScatter.GlyphStyle.Shape = draw.PlusGlyph{}
	rdsScatter.Color = color.RGBA{R: 0, G: 240, B: 108, A: 255}
	p.Legend.Add("RDS CPU", rdsScatter)
	p.Add(rdsScatter)

	ec2Scatter, err := plotter.NewScatter(ec2Data)
	if err != nil {
		return 0, fmt.Errorf("could not create scatter plot: %v", err)
	}
	ec2Scatter.GlyphStyle.Shape = draw.CrossGlyph{}
	ec2Scatter.Color = color.RGBA{R: 90, G: 180, B: 234, A: 255}
	p.Legend.Add("EC2 CPU", ec2Scatter)
	p.Add(ec2Scatter)

	// the linear regression line alpha + beta*x
	rdsC, rdsM := stat.LinearRegression(rdsData.x, rdsData.y, nil, false)
	line, err := addRegressionLine(p, rdsScatter, rdsM, rdsC)
	if err != nil {
		return 0, err
	}
	line.Color = color.RGBA{20, 240, 80, 255}

	// the linear regression line alpha + beta*x
	ec2c, ec2m := stat.LinearRegression(ec2Data.x, ec2Data.y, nil, false)
	line, err = addRegressionLine(p, ec2Scatter, ec2m, ec2c)
	if err != nil {
		return 0, err
	}
	line.Color = color.RGBA{20, 100, 240, 255}

	// a centroid shows the mean of all scatter points and must fall on the regression line
	xMean := stat.Mean(ec2Data.x, nil)
	yMean := stat.Mean(ec2Data.y, nil)
	zMean := stat.Mean(ec2Data.z, nil)
	yStdDev := stat.StdDev(ec2Data.y, nil)
	xStdDev := stat.StdDev(ec2Data.x, nil)

	if err := addCentroid(p, xMean, yMean); err != nil {
		return 0, err
	}

	addTimespanLabel(p, timeRange)

	addLabel(p, fmt.Sprintf("EC2 CPU mean: %0.1f%%/min (stddev: %0.1f)", yMean, yStdDev))
	addLabel(p, fmt.Sprintf("Mean request %0.1f/min (stddev: %0.1f) (%0.0f/mnth)", xMean, xStdDev, xMean*60*24*31))
	addLabel(p, fmt.Sprintf("CPU Cores mean: %0.1f", zMean))
	addLabel(p, fmt.Sprintf("estimated max request per min: %0.1f (RDS: %0.1f)", (100-ec2c)/ec2m, (100-rdsC)/rdsM))
	addLabel(p, fmt.Sprintf("data points: %d", len(ec2Data.x)))

	if err := addVerticalLine(p, ec2Scatter, 100); err != nil {
		return 0, err
	}

	return yMean, nil
}

func addLabel(p *plot.Plot, text string) {
	p.Legend.Add(text)
}

func histogram(p *plot.Plot, in map[int]float64) error {
	frequency := make(map[int]int)
	for _, v := range in {
		intV := int(v)
		_, ok := frequency[intV]
		if !ok {
			frequency[intV] = 0
		}
		frequency[intV] += 1
	}

	data := &xyz{}
	for k, v := range frequency {
		data.x = append(data.x, float64(k))
		data.y = append(data.y, float64(v))
	}

	h, err := plotter.NewHistogram(data, 6)
	if err != nil {
		return fmt.Errorf("could not create histogram: %v", err)
	}

	p.Add(h)

	p.X.Label.Text = "cpu utilisation"
	p.Y.Label.Text = "freq"

	return nil
}

func addVerticalLine(p *plot.Plot, s *plotter.Scatter, y float64) error {
	xMin, xMax, _, _ := s.DataRange()
	maxLine, err := plotter.NewLine(plotter.XYs{{xMin, y}, {xMax, y}})
	if err != nil {
		return fmt.Errorf("could not add maxline: %v", err)
	}
	maxLine.Color = color.RGBA{R: 255, A: 255}
	maxLine.Dashes = []vg.Length{vg.Points(5), vg.Points(5)}
	p.Add(maxLine)
	return nil
}

func addTimespanLabel(p *plot.Plot, tr time.Duration) {
	end := time.Now()
	start := end.Add(-tr)
	format := "2006-01-02 15:04"
	p.Legend.Add(fmt.Sprintf("%s - %s", start.Format(format), end.Format(format)))
}

func addRegressionLine(p *plot.Plot, s *plotter.Scatter, m, c float64) (*plotter.Line, error) {
	min, max, _, _ := s.DataRange()
	l, err := plotter.NewLine(plotter.XYs{
		{min, min*m + c}, {max, max*m + c},
	})
	if err != nil {
		return l, fmt.Errorf("could not create regression lineline: %v", err)
	}
	p.Add(l)
	return l, nil
}

func addCentroid(p *plot.Plot, xMean, yMean float64) error {
	centroidXYs := xyz{
		x: []float64{xMean},
		y: []float64{yMean},
	}
	centroid, err := plotter.NewScatter(centroidXYs)
	if err != nil {
		return fmt.Errorf("could not create scatter: %v", err)
	}
	centroid.GlyphStyle.Shape = draw.CircleGlyph{}
	centroid.GlyphStyle.Radius = 4.0
	p.Add(centroid)
	return nil
}

func createPlot(path, label string) (*os.File, *plot.Plot, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, nil, fmt.Errorf("could not create %s: %v", path, err)
	}

	p, err := plot.New()
	if err != nil {
		return nil, nil, fmt.Errorf("could not create plot: %v", err)
	}
	p.Title.Text = label
	p.Legend.Left = true
	p.Legend.Top = true

	return f, p, nil
}

func writePlot(f *os.File, p *plot.Plot, w, h vg.Length) error {
	wt, err := p.WriterTo(w, h, imageFormat)
	if err != nil {
		return fmt.Errorf("could not create writer: %v", err)
	}
	_, err = wt.WriteTo(f)
	if err != nil {
		return fmt.Errorf("could not write to file %v", err)
	}

	if err := f.Close(); err != nil {
		return fmt.Errorf("could not close output file %v", err)
	}
	return nil
}
