"use client";

import { EXCHANGE_INTERVALS, exchangeDecimals, useExchangeCandles, type ExchangeCandle, type ExchangeCandleInterval, type ExchangeMarket } from "@scaffoldapp/shared";
import { ErrorState, SegmentedControl, Skeleton, Spinner, cn, useColorMode, usePalette } from "@scaffoldapp/ui-web";
import type { IChartApi, ISeriesApi, LogicalRange, UTCTimestamp } from "lightweight-charts";
import { useEffect, useRef, useState } from "react";

// lightweight-charts loads on demand, keeping it out of every other route's bundle.
type LC = typeof import("lightweight-charts");
let lcPromise: Promise<LC> | null = null;
const loadLC = () => (lcPromise ??= import("lightweight-charts"));

const LABELS: Record<ExchangeCandleInterval, string> = { "1m": "1m", "5m": "5m", "15m": "15m", "1h": "1h", "4h": "4h", "1d": "1D" };

interface Palette {
  text: string;
  grid: string;
  border: string;
  up: string;
  down: string;
  upSoft: string;
  downSoft: string;
  crosshair: string;
  label: string;
  font: string;
}

let probe: CanvasRenderingContext2D | null = null;

/** A token colour as rgba() (the chart can't parse oklch or color-mix). */
function rgba(css: string, alpha = 1): string {
  probe ??= document.createElement("canvas").getContext("2d", { willReadFrequently: true });
  if (!probe || !css) return `rgba(128,128,128,${alpha})`;
  probe.clearRect(0, 0, 1, 1);
  probe.fillStyle = "rgba(0,0,0,0)";
  probe.fillStyle = css;
  probe.fillRect(0, 0, 1, 1);
  const [r = 0, g = 0, b = 0, a = 255] = probe.getImageData(0, 0, 1, 1).data;
  return `rgba(${r},${g},${b},${((a / 255) * alpha).toFixed(3)})`;
}

function readPalette(): Palette {
  const cs = getComputedStyle(document.documentElement);
  const v = (n: string) => cs.getPropertyValue(n).trim();
  return {
    text: rgba(v("--app-text-muted")),
    grid: rgba(v("--app-border"), 0.6),
    border: rgba(v("--app-border")),
    up: rgba(v("--app-success")),
    down: rgba(v("--app-danger")),
    upSoft: rgba(v("--app-success"), 0.3),
    downSoft: rgba(v("--app-danger"), 0.3),
    crosshair: rgba(v("--app-border-strong")),
    label: rgba(v("--app-surface-raised")),
    font: getComputedStyle(document.body).fontFamily,
  };
}

const toSec = (ms: number) => Math.floor(ms / 1000) as UTCTimestamp;

function bar(c: ExchangeCandle) {
  return { time: toSec(c.t), open: Number(c.o), high: Number(c.h), low: Number(c.l), close: Number(c.c) };
}

/**
 * Candles and volume for one market: interval picker, live last bar over the market's
 * topic, and older history loaded as the view scrolls toward the left edge.
 */
export function PriceChart({ market, className }: { market: ExchangeMarket; className?: string }) {
  const [interval, setIv] = useState<ExchangeCandleInterval>("5m");
  const data = useExchangeCandles(market.symbol, interval);
  const el = useRef<HTMLDivElement>(null);
  const chartRef = useRef<IChartApi | null>(null);
  const candleRef = useRef<ISeriesApi<"Candlestick"> | null>(null);
  const volRef = useRef<ISeriesApi<"Histogram"> | null>(null);
  const palRef = useRef<Palette | null>(null);
  const drawn = useRef<{ first: number; last: number; n: number }>({ first: 0, last: 0, n: 0 });
  const olderRef = useRef(data.fetchOlder);
  olderRef.current = data.hasOlder ? data.fetchOlder : () => {};
  const [ready, setReady] = useState(false);
  const { mode } = useColorMode();
  const { id: palette } = usePalette();
  const decimals = exchangeDecimals(market.tick_size);

  // Create the chart once per market.
  useEffect(() => {
    let disposed = false;
    let cleanup = () => {};
    void loadLC().then((lc) => {
      if (disposed || !el.current) return;
      const p = (palRef.current = readPalette());
      const chart = lc.createChart(el.current, {
        autoSize: true,
        layout: { background: { type: lc.ColorType.Solid, color: "rgba(0,0,0,0)" }, textColor: p.text, fontFamily: p.font, fontSize: 11, attributionLogo: false },
        grid: { vertLines: { color: p.grid }, horzLines: { color: p.grid } },
        rightPriceScale: { borderColor: p.border, scaleMargins: { top: 0.12, bottom: 0.22 } },
        timeScale: { borderColor: p.border, timeVisible: true, secondsVisible: false, rightOffset: 4, barSpacing: 8 },
        crosshair: { mode: lc.CrosshairMode.Normal, vertLine: { color: p.crosshair, labelBackgroundColor: p.label }, horzLine: { color: p.crosshair, labelBackgroundColor: p.label } },
      });
      const candles = chart.addSeries(lc.CandlestickSeries, {
        upColor: p.up,
        downColor: p.down,
        borderVisible: false,
        wickUpColor: p.up,
        wickDownColor: p.down,
        priceFormat: { type: "price", precision: decimals, minMove: Number(market.tick_size) || Math.pow(10, -decimals) },
      });
      const vol = chart.addSeries(lc.HistogramSeries, { priceScaleId: "vol", priceFormat: { type: "volume" }, lastValueVisible: false, priceLineVisible: false });
      chart.priceScale("vol").applyOptions({ scaleMargins: { top: 0.82, bottom: 0 } });
      const onRange = (range: LogicalRange | null) => {
        if (range && range.from < 20) olderRef.current();
      };
      chart.timeScale().subscribeVisibleLogicalRangeChange(onRange);
      chartRef.current = chart;
      candleRef.current = candles;
      volRef.current = vol;
      drawn.current = { first: 0, last: 0, n: 0 };
      setReady(true);
      cleanup = () => {
        chart.timeScale().unsubscribeVisibleLogicalRangeChange(onRange);
        chart.remove();
      };
    });
    return () => {
      disposed = true;
      cleanup();
      chartRef.current = candleRef.current = volRef.current = null;
      setReady(false);
    };
  }, [market.symbol, market.tick_size, decimals]);

  // A new interval starts from a clean slate.
  useEffect(() => {
    drawn.current = { first: 0, last: 0, n: 0 };
  }, [interval]);

  // Re-theme on mode or palette change.
  useEffect(() => {
    const chart = chartRef.current;
    if (!chart || !ready) return;
    const p = (palRef.current = readPalette());
    chart.applyOptions({
      layout: { textColor: p.text },
      grid: { vertLines: { color: p.grid }, horzLines: { color: p.grid } },
      rightPriceScale: { borderColor: p.border },
      timeScale: { borderColor: p.border },
      crosshair: { vertLine: { color: p.crosshair, labelBackgroundColor: p.label }, horzLine: { color: p.crosshair, labelBackgroundColor: p.label } },
    });
    candleRef.current?.applyOptions({ upColor: p.up, downColor: p.down, wickUpColor: p.up, wickDownColor: p.down });
    drawn.current = { first: 0, last: 0, n: 0 }; // redraw volume colours
  }, [mode, palette, ready]);

  // Draw: the live bar as an update, anything else as a full set (keeping the view put).
  useEffect(() => {
    const chart = chartRef.current;
    const cs = candleRef.current;
    const vs = volRef.current;
    const p = palRef.current;
    if (!chart || !cs || !vs || !p || !ready) return;
    const list = data.candles;
    if (list.length === 0) {
      cs.setData([]);
      vs.setData([]);
      drawn.current = { first: 0, last: 0, n: 0 };
      return;
    }
    const first = list[0]?.t ?? 0;
    const last = list[list.length - 1]!;
    const prev = drawn.current;
    const vol = (c: ExchangeCandle) => ({ time: toSec(c.t), value: Number(c.v), color: Number(c.c) >= Number(c.o) ? p.upSoft : p.downSoft });
    if (prev.n > 0 && first === prev.first && (list.length === prev.n || list.length === prev.n + 1) && last.t >= prev.last) {
      cs.update(bar(last));
      vs.update(vol(last));
    } else {
      const range = chart.timeScale().getVisibleLogicalRange();
      const prepended = prev.n > 0 && first < prev.first ? list.findIndex((c) => c.t === prev.first) : 0;
      cs.setData(list.map(bar));
      vs.setData(list.map(vol));
      if (range && prepended > 0) chart.timeScale().setVisibleLogicalRange({ from: range.from + prepended, to: range.to + prepended });
    }
    drawn.current = { first, last: last.t, n: list.length };
  }, [data.candles, ready]);

  return (
    <div className={cn("flex flex-col gap-3", className)}>
      <div className="flex items-center justify-between gap-3">
        <SegmentedControl<ExchangeCandleInterval>
          options={EXCHANGE_INTERVALS.map((v) => ({ value: v, label: LABELS[v] }))}
          value={interval}
          onValueChange={setIv}
          fullWidth={false}
          className="max-w-full overflow-x-auto"
        />
        {data.isFetchingOlder && <Spinner size={16} label="Loading earlier candles" />}
      </div>
      <div className="relative h-72 w-full sm:h-96">
        <div ref={el} className="absolute inset-0" aria-label={`${market.name} price chart, ${LABELS[interval]} candles`} role="img" />
        {data.isPending ? (
          <Skeleton className="absolute inset-0" />
        ) : data.isError ? (
          <div className="absolute inset-0 flex items-center justify-center bg-canvas">
            <ErrorState compact title="Couldn't load the chart" info={data.error?.message} onRetry={data.refetch} />
          </div>
        ) : data.candles.length === 0 ? (
          <div className="absolute inset-0 flex items-center justify-center type-label text-fg-muted">No trading history yet</div>
        ) : null}
      </div>
    </div>
  );
}
