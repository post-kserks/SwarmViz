import React from 'react';
import {
  AreaChart,
  Area,
  XAxis,
  YAxis,
  Tooltip,
  ResponsiveContainer,
} from 'recharts';
import { Activity } from 'lucide-react';
import { useLocHistory } from '../../store/useSwarmStore';
import { LocPoint } from '../../types/swarm';

const CustomTooltip: React.FC<any> = ({ active, payload }) => {
  if (active && payload && payload.length) {
    const data: LocPoint = payload[0].payload;
    return (
      <div className="bg-[#12141c] border border-[#1e2230] p-2.5 rounded-lg shadow-xl text-xs font-sans">
        <p className="text-slate-400 mb-1">{data.ts}</p>
        <div className="flex flex-col gap-0.5 font-mono">
          <p className="text-emerald-400">Added: +{data.added} LOC</p>
          <p className="text-rose-400">Removed: -{data.removed} LOC</p>
          <p className="text-sky-400 font-semibold border-t border-[#1e2230] pt-1 mt-1">
            Net Delta: {data.netDelta >= 0 ? `+${data.netDelta}` : data.netDelta} LOC
          </p>
        </div>
      </div>
    );
  }
  return null;
};

export const LocChart: React.FC = () => {
  const locHistory = useLocHistory();

  // Calculate totals
  const latestPoint = locHistory.length > 0 ? locHistory[locHistory.length - 1] : null;
  const totalAdded = latestPoint ? latestPoint.added : 0;
  const totalRemoved = latestPoint ? latestPoint.removed : 0;
  const netDelta = totalAdded - totalRemoved;

  return (
    <div className="flex flex-col h-full w-full bg-[#12141c] border border-[#1e2230] rounded-lg p-3 shadow-lg overflow-hidden">
      {/* Header */}
      <div className="flex items-center justify-between pb-2 border-b border-[#1e2230] mb-2 shrink-0">
        <div className="flex items-center gap-2">
          <Activity className="w-4 h-4 text-sky-400" />
          <h3 className="text-sm font-semibold text-slate-200">Lines of Code Delta</h3>
        </div>

        {/* Cumulative Stats Badge */}
        <div className="flex items-center gap-2 font-mono text-xs">
          <span className="text-slate-400">Delta:</span>
          <span className="text-emerald-400 font-semibold">+{totalAdded}</span>
          <span className="text-slate-600">/</span>
          <span className="text-rose-400 font-semibold">-{totalRemoved}</span>
          <span className="text-sky-400 font-bold ml-1 bg-sky-950/40 border border-sky-500/30 px-1.5 py-0.5 rounded">
            {netDelta >= 0 ? `+${netDelta}` : netDelta} LOC
          </span>
        </div>
      </div>

      {/* Chart Canvas */}
      <div className="flex-1 w-full min-h-0">
        {locHistory.length > 0 ? (
          <ResponsiveContainer width="100%" height="100%">
            <AreaChart data={locHistory} margin={{ top: 5, right: 10, left: -20, bottom: 0 }}>
              <defs>
                <linearGradient id="locAreaGradient" x1="0" y1="0" x2="0" y2="1">
                  <stop offset="0%" stopColor="#38bdf8" stopOpacity={0.4} />
                  <stop offset="100%" stopColor="#38bdf8" stopOpacity={0.0} />
                </linearGradient>
              </defs>
              <XAxis
                dataKey="ts"
                tick={{ fill: '#64748b', fontSize: 10 }}
                tickLine={false}
                axisLine={{ stroke: '#1e2230' }}
              />
              <YAxis
                tick={{ fill: '#64748b', fontSize: 10 }}
                tickLine={false}
                axisLine={{ stroke: '#1e2230' }}
              />
              <Tooltip content={<CustomTooltip />} />
              <Area
                type="monotone"
                dataKey="netDelta"
                stroke="#38bdf8"
                strokeWidth={2}
                fillOpacity={1}
                fill="url(#locAreaGradient)"
                isAnimationActive={false}
              />
            </AreaChart>
          </ResponsiveContainer>
        ) : (
          <div className="h-full flex items-center justify-center text-xs text-slate-500 italic">
            Waiting for LOC delta events...
          </div>
        )}
      </div>
    </div>
  );
};
