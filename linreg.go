package main

import (
	"gonum.org/v1/gonum/stat"
)

func linearRegression(x, y []float64) (m, c float64) {
	c, m = stat.LinearRegression(x, y, nil, false)
	return m, c
}

// Sum Square Errors
func computeSSE(x, y []float64, m, c float64) float64 {
	s := 0.0
	for i := range x {
		d := y[i] - (x[i]*m + c)
		s += d * d
	}
	return s
}

// Sum Square of Total
func computeSST(x, y []float64) float64 {
	m := stat.Mean(y, nil)
	s := 0.0
	for i := range x {
		d := y[i] - m
		s += d * d
	}
	return s
}

// Sum Square errors due to regression
func computeSSR(x, y []float64, m, c float64) float64 {
	mean := stat.Mean(y, nil)
	s := 0.0
	for i := range x {
		ybar := y[i] - mean
		yhat := y[i] - (x[i]*m + c)
		d := yhat - ybar
		s += d * d
	}
	return s
}

func computeCost(x, y []float64, m, c float64) float64 {
	// cost = 1/N * sum((y - (m*x+c))^2)
	s := 0.0
	for i := range x {
		d := y[i] - (x[i]*m + c)
		s += d * d
	}
	return s / float64(len(x))
}

func computeGradient(x, y []float64, m, c float64) (dm, dc float64) {
	// cost = 1/N * sum((y - (m*x+c))^2)
	// cost/dm = 2/N * sum(-x * (y - (m*x+c)))
	// cost/dc = 2/N * sum(-(y - (m*x+c)))

	for i := range x {
		d := y[i] - (x[i]*m + c)
		dm -= x[i] * d
		dc -= d
	}
	n := float64(len(x))
	return 2 / n * dm, 2 / n * dc
}
