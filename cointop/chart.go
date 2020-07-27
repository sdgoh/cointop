package cointop

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/miguelmota/cointop/cointop/common/filecache"
	"github.com/miguelmota/cointop/cointop/common/gizak/termui"
	"github.com/miguelmota/cointop/cointop/common/timeutil"
)

// ChartView is structure for chart view
type ChartView struct {
	*View
}

// NewChartView returns a new chart view
func NewChartView() *ChartView {
	return &ChartView{NewView("chart")}
}

var chartLock sync.Mutex
var chartPointsLock sync.Mutex

// ChartRanges returns list of chart ranges available
func ChartRanges() []string {
	return []string{
		"24H",
		"3D",
		"7D",
		"1M",
		"3M",
		"6M",
		"1Y",
		"YTD",
		"All Time",
	}
}

// ChartRanges returns map of chart range time ranges
func ChartRangesMap() map[string]time.Duration {
	return map[string]time.Duration{
		"All Time": time.Duration(24 * 7 * 4 * 12 * 5 * time.Hour),
		"YTD":      time.Duration(1 * time.Second), // this will be calculated
		"1Y":       time.Duration(24 * 7 * 4 * 12 * time.Hour),
		"6M":       time.Duration(24 * 7 * 4 * 6 * time.Hour),
		"3M":       time.Duration(24 * 7 * 4 * 3 * time.Hour),
		"1M":       time.Duration(24 * 7 * 4 * time.Hour),
		"7D":       time.Duration(24 * 7 * time.Hour),
		"3D":       time.Duration(24 * 3 * time.Hour),
		"24H":      time.Duration(24 * time.Hour),
		"6H":       time.Duration(6 * time.Hour),
		"1H":       time.Duration(1 * time.Hour),
	}
}

// UpdateChart updates the chart view
func (ct *Cointop) UpdateChart() error {
	ct.debuglog("UpdateChart()")
	if ct.Views.Chart.Backing() == nil {
		return nil
	}

	chartLock.Lock()
	defer chartLock.Unlock()

	if ct.State.portfolioVisible {
		if err := ct.PortfolioChart(); err != nil {
			return err
		}
	} else {
		symbol := ct.SelectedCoinSymbol()
		name := ct.SelectedCoinName()
		ct.ChartPoints(symbol, name)
	}

	var body string
	if len(ct.State.chartPoints) == 0 {
		body = "\n\n\n\n\nnot enough data for chart"
	} else {
		for i := range ct.State.chartPoints {
			var s string
			for j := range ct.State.chartPoints[i] {
				p := ct.State.chartPoints[i][j]
				s = fmt.Sprintf("%s%c", s, p.Ch)
			}
			body = fmt.Sprintf("%s%s\n", body, s)

		}
	}

	ct.Update(func() error {
		if ct.Views.Chart.Backing() == nil {
			return nil
		}

		ct.Views.Chart.Backing().Clear()
		fmt.Fprint(ct.Views.Chart.Backing(), ct.colorscheme.Chart(body))
		return nil
	})

	return nil
}

// ChartPoints calculates the the chart points
func (ct *Cointop) ChartPoints(symbol string, name string) error {
	ct.debuglog("ChartPoints()")
	maxX := ct.ClampedWidth()

	chartPointsLock.Lock()
	defer chartPointsLock.Unlock()

	// TODO: not do this (SoC)
	go ct.UpdateMarketbar()

	chart := termui.NewLineChart()
	chart.Height = ct.State.chartHeight
	chart.Border = false

	// NOTE: empty list means don't show x-axis labels
	chart.DataLabels = []string{""}

	rangeseconds := ct.chartRangesMap[ct.State.selectedChartRange]
	if ct.State.selectedChartRange == "YTD" {
		ytd := time.Now().Unix() - int64(timeutil.BeginningOfYear().Unix())
		rangeseconds = time.Duration(ytd) * time.Second
	}

	now := time.Now()
	nowseconds := now.Unix()
	start := nowseconds - int64(rangeseconds.Seconds())
	end := nowseconds

	var data []float64

	keyname := symbol
	if keyname == "" {
		keyname = "globaldata"
	}
	cachekey := ct.CacheKey(fmt.Sprintf("%s_%s", keyname, strings.Replace(ct.State.selectedChartRange, " ", "", -1)))

	cached, found := ct.cache.Get(cachekey)
	if found {
		// cache hit
		data, _ = cached.([]float64)
		ct.debuglog("soft cache hit")
	}

	if len(data) == 0 {
		if symbol == "" {
			convert := ct.State.currencyConversion
			graphData, err := ct.api.GetGlobalMarketGraphData(convert, start, end)
			if err != nil {
				return nil
			}
			for i := range graphData.MarketCapByAvailableSupply {
				price := graphData.MarketCapByAvailableSupply[i][1]
				data = append(data, price/1e9)
			}
		} else {
			convert := ct.State.currencyConversion
			graphData, err := ct.api.GetCoinGraphData(convert, symbol, name, start, end)
			if err != nil {
				return nil
			}

			// NOTE: edit `termui.LineChart.shortenFloatVal(float64)` to not
			// use exponential notation.
			for i := range graphData.Price {
				price := graphData.Price[i][1]
				data = append(data, price)
			}
		}

		ct.cache.Set(cachekey, data, 10*time.Second)
		go func() {
			filecache.Set(cachekey, data, 24*time.Hour)
		}()
	}

	chart.Data = data
	termui.Body = termui.NewGrid()
	termui.Body.Width = maxX
	termui.Body.AddRows(
		termui.NewRow(
			termui.NewCol(12, 0, chart),
		),
	)

	var points [][]termui.Cell
	// calculate layout
	termui.Body.Align()
	w := termui.Body.Width
	h := chart.Height
	row := termui.Body.Rows[0]
	b := row.Buffer()
	for i := 0; i < h; i = i + 1 {
		var rowpoints []termui.Cell
		for j := 0; j < w; j = j + 1 {
			p := b.At(j, i)
			rowpoints = append(rowpoints, p)
		}
		points = append(points, rowpoints)
	}

	ct.State.chartPoints = points

	return nil
}

// PortfolioChart renders the portfolio chart
func (ct *Cointop) PortfolioChart() error {
	ct.debuglog("PortfolioChart()")
	maxX := ct.ClampedWidth()
	chartPointsLock.Lock()
	defer chartPointsLock.Unlock()

	// TODO: not do this (SoC)
	go ct.UpdateMarketbar()

	chart := termui.NewLineChart()
	chart.Height = ct.State.chartHeight
	chart.Border = false

	// NOTE: empty list means don't show x-axis labels
	chart.DataLabels = []string{""}

	rangeseconds := ct.chartRangesMap[ct.State.selectedChartRange]
	if ct.State.selectedChartRange == "YTD" {
		ytd := time.Now().Unix() - int64(timeutil.BeginningOfYear().Unix())
		rangeseconds = time.Duration(ytd) * time.Second
	}

	now := time.Now()
	nowseconds := now.Unix()
	start := nowseconds - int64(rangeseconds.Seconds())
	end := nowseconds

	var data []float64
	portfolio := ct.GetPortfolioSlice()
	chartname := ct.SelectedCoinName()
	for _, p := range portfolio {
		// filter by selected chart if selected
		if chartname != "" {
			if chartname != p.Name {
				continue
			}
		}

		if p.Holdings <= 0 {
			continue
		}

		var graphData []float64
		cachekey := strings.ToLower(fmt.Sprintf("%s_%s", p.Symbol, strings.Replace(ct.State.selectedChartRange, " ", "", -1)))
		cached, found := ct.cache.Get(cachekey)
		if found {
			// cache hit
			graphData, _ = cached.([]float64)
			ct.debuglog("soft cache hit")
		} else {
			filecache.Get(cachekey, &graphData)

			if len(graphData) == 0 {
				time.Sleep(2 * time.Second)

				convert := ct.State.currencyConversion
				apiGraphData, err := ct.api.GetCoinGraphData(convert, p.Symbol, p.Name, start, end)
				if err != nil {
					return err
				}
				for i := range apiGraphData.Price {
					price := apiGraphData.Price[i][1]
					graphData = append(graphData, price)
				}
			}

			ct.cache.Set(cachekey, graphData, 10*time.Second)
			go func() {
				filecache.Set(cachekey, graphData, 24*time.Hour)
			}()
		}

		for i := range graphData {
			price := graphData[i]
			sum := p.Holdings * price
			if len(data)-1 >= i {
				data[i] += sum
			}
			data = append(data, sum)
		}
	}

	chart.Data = data
	termui.Body = termui.NewGrid()
	termui.Body.Width = maxX
	termui.Body.AddRows(
		termui.NewRow(
			termui.NewCol(12, 0, chart),
		),
	)

	var points [][]termui.Cell
	// calculate layout
	termui.Body.Align()
	w := termui.Body.Width
	h := chart.Height
	row := termui.Body.Rows[0]
	b := row.Buffer()
	for i := 0; i < h; i = i + 1 {
		var rowpoints []termui.Cell
		for j := 0; j < w; j = j + 1 {
			p := b.At(j, i)
			rowpoints = append(rowpoints, p)
		}
		points = append(points, rowpoints)
	}

	ct.State.chartPoints = points

	return nil
}

// ShortenChart decreases the chart height by one row
func (ct *Cointop) ShortenChart() error {
	ct.debuglog("ShortenChart()")
	candidate := ct.State.chartHeight - 1
	if candidate < 5 {
		return nil
	}
	ct.State.chartHeight = candidate

	go ct.UpdateChart()
	return nil
}

// EnlargeChart increases the chart height by one row
func (ct *Cointop) EnlargeChart() error {
	ct.debuglog("EnlargeChart()")
	candidate := ct.State.chartHeight + 1
	if candidate > 30 {
		return nil
	}
	ct.State.chartHeight = candidate

	go ct.UpdateChart()
	return nil
}

// NextChartRange sets the chart to the next range option
func (ct *Cointop) NextChartRange() error {
	ct.debuglog("NextChartRange()")
	sel := 0
	max := len(ct.chartRanges)
	for i, k := range ct.chartRanges {
		if k == ct.State.selectedChartRange {
			sel = i + 1
			break
		}
	}
	if sel > max-1 {
		sel = 0
	}

	ct.State.selectedChartRange = ct.chartRanges[sel]

	go ct.UpdateChart()
	return nil
}

// PrevChartRange sets the chart to the prevous range option
func (ct *Cointop) PrevChartRange() error {
	ct.debuglog("PrevChartRange()")
	sel := 0
	for i, k := range ct.chartRanges {
		if k == ct.State.selectedChartRange {
			sel = i - 1
			break
		}
	}
	if sel < 0 {
		sel = len(ct.chartRanges) - 1
	}

	ct.State.selectedChartRange = ct.chartRanges[sel]
	go ct.UpdateChart()
	return nil
}

// FirstChartRange sets the chart to the first range option
func (ct *Cointop) FirstChartRange() error {
	ct.debuglog("FirstChartRange()")
	ct.State.selectedChartRange = ct.chartRanges[0]
	go ct.UpdateChart()
	return nil
}

// LastChartRange sets the chart to the last range option
func (ct *Cointop) LastChartRange() error {
	ct.debuglog("LastChartRange()")
	ct.State.selectedChartRange = ct.chartRanges[len(ct.chartRanges)-1]
	go ct.UpdateChart()
	return nil
}

// ToggleCoinChart toggles between the global chart and the coin chart
func (ct *Cointop) ToggleCoinChart() error {
	ct.debuglog("ToggleCoinChart()")
	highlightedcoin := ct.HighlightedRowCoin()
	if ct.State.selectedCoin == highlightedcoin {
		ct.State.selectedCoin = nil
	} else {
		ct.State.selectedCoin = highlightedcoin
	}

	go func() {
		// keep these two synchronous to avoid race conditions
		ct.ShowChartLoader()
		ct.UpdateChart()
	}()

	go ct.UpdateMarketbar()

	return nil
}

// ShowChartLoader shows chart loading indicator
func (ct *Cointop) ShowChartLoader() error {
	ct.debuglog("ShowChartLoader()")
	ct.Update(func() error {
		if ct.Views.Chart.Backing() == nil {
			return nil
		}

		content := "\n\nLoading..."
		ct.Views.Chart.Backing().Clear()
		fmt.Fprint(ct.Views.Chart.Backing(), ct.colorscheme.Chart(content))
		return nil
	})

	return nil
}
