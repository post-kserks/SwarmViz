import { LocPoint } from '../types/swarm';

/**
 * Largest Triangle Three Buckets (LTTB) Downsampling algorithm.
 * Reduces raw time-series data to `threshold` points while preserving original shape, peaks, and troughs.
 */
export function lttbDownsample(data: LocPoint[], threshold: number): LocPoint[] {
  if (threshold >= data.length || threshold <= 0) {
    return data;
  }

  const sampled: LocPoint[] = [];
  let sampledIndex = 0;

  // Bucket size calculation
  const every = (data.length - 2) / (threshold - 2);

  let a = 0; // First point index
  sampled[sampledIndex++] = data[a];

  for (let i = 0; i < threshold - 2; i++) {
    // Calculate point average for next bucket (Bucket B)
    let avgX = 0;
    let avgY = 0;
    let avgRangeStart = Math.floor((i + 1) * every) + 1;
    let avgRangeEnd = Math.floor((i + 2) * every) + 1;
    avgRangeEnd = avgRangeEnd < data.length ? avgRangeEnd : data.length;

    const avgRangeLength = avgRangeEnd - avgRangeStart;

    for (; avgRangeStart < avgRangeEnd; avgRangeStart++) {
      avgX += avgRangeStart;
      avgY += data[avgRangeStart].netDelta;
    }
    if (avgRangeLength > 0) {
      avgX /= avgRangeLength;
      avgY /= avgRangeLength;
    }

    // Get the range for current bucket (Bucket A)
    let rangeOffs = Math.floor((i + 0) * every) + 1;
    const rangeTo = Math.floor((i + 1) * every) + 1;

    // Point A
    const pointAX = a;
    const pointAY = data[a].netDelta;

    let maxArea = -1;
    let maxAreaPoint = rangeOffs;

    for (; rangeOffs < rangeTo && rangeOffs < data.length; rangeOffs++) {
      // Calculate triangle area
      const area = Math.abs(
        (pointAX - avgX) * (data[rangeOffs].netDelta - pointAY) -
        (pointAX - rangeOffs) * (avgY - pointAY)
      ) * 0.5;

      if (area > maxArea) {
        maxArea = area;
        maxAreaPoint = rangeOffs;
      }
    }

    if (maxAreaPoint < data.length) {
      sampled[sampledIndex++] = data[maxAreaPoint];
      a = maxAreaPoint;
    }
  }

  // Always include last point
  if (data.length > 0) {
    sampled[sampledIndex++] = data[data.length - 1];
  }

  return sampled;
}
