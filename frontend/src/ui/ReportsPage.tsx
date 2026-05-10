import { useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { api } from "../utils/api";

type CollectionReport = Awaited<ReturnType<typeof api.collectionReport>>;

function navigate(to: string) {
  window.location.hash = to.startsWith("#") ? to : `#${to}`;
}

function StatBox(props: { label: string; value: string | number; sub?: string; accent?: boolean }) {
  return (
    <div className={`rounded-lg border p-4 ${props.accent ? "border-red-500/30 bg-red-500/5" : "border-zinc-200 bg-white dark:border-white/10 dark:bg-white/[0.04]"}`}>
      <div className="text-2xl font-bold">{props.value}</div>
      <div className="mt-0.5 text-sm font-medium">{props.label}</div>
      {props.sub ? <div className="mt-1 text-xs text-zinc-500 dark:text-zinc-400">{props.sub}</div> : null}
    </div>
  );
}

function SpinsBarChart(props: { data: Array<{ period: string; spin_count: number }>; period: "week" | "month" }) {
  const { data, period } = props;
  if (!data.length) return <div className="text-sm text-zinc-500">No spins recorded yet.</div>;

  const max = Math.max(...data.map((d) => d.spin_count), 1);

  // Show a label every N bars so the x-axis isn't crowded
  const labelEvery = period === "month" ? 3 : 8;

  function formatLabel(iso: string) {
    const d = new Date(iso + "T00:00:00Z");
    if (period === "month") {
      return d.toLocaleDateString("en-US", { month: "short", year: "2-digit", timeZone: "UTC" });
    }
    return d.toLocaleDateString("en-US", { month: "short", day: "numeric", timeZone: "UTC" });
  }

  return (
    <div>
      <div className="flex items-end gap-px" style={{ height: "120px" }}>
        {data.map((d, i) => {
          const h = Math.max((d.spin_count / max) * 100, d.spin_count > 0 ? 2 : 0);
          return (
            <div
              key={d.period}
              className="group relative flex-1"
              style={{ height: "100%" }}
            >
              {/* Tooltip */}
              <div className="pointer-events-none absolute bottom-full left-1/2 z-10 mb-1 hidden -translate-x-1/2 whitespace-nowrap rounded bg-zinc-800 px-2 py-1 text-xs text-white group-hover:block dark:bg-zinc-100 dark:text-zinc-900">
                {formatLabel(d.period)}: {d.spin_count} {d.spin_count === 1 ? "spin" : "spins"}
              </div>
              {/* Bar */}
              <div className="absolute bottom-0 left-0 right-0 rounded-t-sm bg-red-500/70 hover:bg-red-500 transition-colors" style={{ height: `${h}%` }} />
              {/* Zero-height placeholder so flex layout is stable */}
              {d.spin_count === 0 && (
                <div className="absolute bottom-0 left-0 right-0 h-px bg-zinc-200 dark:bg-white/5" />
              )}
            </div>
          );
        })}
      </div>
      {/* X-axis labels */}
      <div className="mt-1 flex gap-px">
        {data.map((d, i) => (
          <div key={d.period} className="flex-1 min-w-0">
            {i % labelEvery === 0 ? (
              <div className="truncate text-left text-[10px] text-zinc-400">{formatLabel(d.period)}</div>
            ) : null}
          </div>
        ))}
      </div>
    </div>
  );
}

function RecordRow(props: {
  id: string;
  artist: string;
  title: string;
  year?: number;
  thumbUrl?: string;
  lastSpunAt?: string;
  spinCount?: number;
}) {
  return (
    <li
      className="flex cursor-pointer items-center gap-3 rounded-md px-2 py-2 hover:bg-zinc-100 dark:hover:bg-white/[0.04]"
      onClick={() => navigate(`/records/${props.id}`)}
    >
      {props.thumbUrl ? (
        <img src={props.thumbUrl} alt="" className="h-8 w-8 shrink-0 rounded object-cover" />
      ) : (
        <div className="h-8 w-8 shrink-0 rounded bg-zinc-100 dark:bg-white/[0.06]" />
      )}
      <div className="min-w-0 flex-1">
        <div className="truncate text-sm font-medium">{props.artist}</div>
        <div className="truncate text-xs text-zinc-500 dark:text-zinc-400">{props.title}{props.year ? ` (${props.year})` : ""}</div>
      </div>
      {props.lastSpunAt ? (
        <div className="shrink-0 text-right text-xs text-zinc-400">
          <div>{props.spinCount} {props.spinCount === 1 ? "spin" : "spins"}</div>
          <div className="text-zinc-500">{new Date(props.lastSpunAt).toLocaleDateString()}</div>
        </div>
      ) : null}
    </li>
  );
}

function YearBarChart(props: { data: Array<{ year: number; count: number }>; label: string }) {
  const { data } = props;
  if (!data.length) return <div className="text-sm text-zinc-500">No data yet.</div>;

  const max = Math.max(...data.map((d) => d.count), 1);

  return (
    <div>
      <div className="flex items-end gap-px" style={{ height: "100px" }}>
        {data.map((d) => {
          const h = Math.max((d.count / max) * 100, d.count > 0 ? 2 : 0);
          return (
            <div key={d.year} className="group relative flex-1" style={{ height: "100%" }}>
              <div className="pointer-events-none absolute bottom-full left-1/2 z-10 mb-1 hidden -translate-x-1/2 whitespace-nowrap rounded bg-zinc-800 px-2 py-1 text-xs text-white group-hover:block dark:bg-zinc-100 dark:text-zinc-900">
                {d.year}: {d.count} {d.count === 1 ? "record" : "records"}
              </div>
              <div className="absolute bottom-0 left-0 right-0 rounded-t-sm bg-red-500/70 hover:bg-red-500 transition-colors" style={{ height: `${h}%` }} />
            </div>
          );
        })}
      </div>
      <div className="mt-1 flex gap-px">
        {data.map((d, i) => (
          <div key={d.year} className="flex-1 min-w-0">
            {i % Math.max(1, Math.floor(data.length / 6)) === 0 ? (
              <div className="truncate text-left text-[10px] text-zinc-400">{d.year}</div>
            ) : null}
          </div>
        ))}
      </div>
    </div>
  );
}

function CountList(props: {
  data: Array<{ label: string; count: number }>;
  emptyMsg: string;
}) {
  const { data } = props;
  if (!data.length) return <div className="text-sm text-zinc-500">{props.emptyMsg}</div>;

  const max = data[0]?.count ?? 1;
  return (
    <ul className="space-y-2">
      {data.map((item, i) => {
        const barPct = max > 0 ? (item.count / max) * 100 : 0;
        return (
          <li key={item.label} className="flex items-center gap-3">
            <div className="w-5 shrink-0 text-right text-xs text-zinc-400">{i + 1}</div>
            <div className="min-w-0 flex-1">
              <div className="mb-1 flex items-baseline justify-between gap-2">
                <span className="truncate text-sm font-medium">{item.label}</span>
                <span className="shrink-0 text-xs text-zinc-400">
                  {item.count} {item.count === 1 ? "record" : "records"}
                </span>
              </div>
              <div className="h-1.5 w-full overflow-hidden rounded-full bg-zinc-100 dark:bg-white/[0.06]">
                <div className="h-full rounded-full bg-red-500" style={{ width: `${barPct}%` }} />
              </div>
            </div>
          </li>
        );
      })}
    </ul>
  );
}

function CollectionReportSections(props: { data: CollectionReport }) {
  const { data } = props;
  const [yearTab, setYearTab] = useState<"pressing" | "original">("pressing");

  const formatData = data.by_format.map((f) => ({ label: f.format, count: f.count }));
  const artistData = data.by_artist.map((a) => ({ label: a.artist, count: a.count }));
  const labelData = data.by_label.map((l) => ({ label: l.label, count: l.count }));

  return (
    <>
      {/* ── Format breakdown ── */}
      <section>
        <h2 className="mb-3 text-sm font-semibold uppercase tracking-wider text-zinc-400 dark:text-zinc-500">
          By Format
        </h2>
        <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
          {formatData.map((f) => (
            <StatBox key={f.label} label={f.label} value={f.count} />
          ))}
          {!formatData.length && (
            <div className="col-span-4 text-sm text-zinc-500">
              Sync your collection to populate format data.
            </div>
          )}
        </div>
      </section>

      {/* ── Records by year ── */}
      <section className="rounded-lg border border-zinc-200 bg-white p-4 dark:border-white/10 dark:bg-white/[0.04]">
        <div className="mb-4 flex items-center justify-between gap-2">
          <h2 className="text-sm font-semibold">Records by year</h2>
          <div className="flex rounded-md border border-zinc-200 text-xs dark:border-white/10">
            <button
              className={`px-3 py-1.5 rounded-l-md transition-colors ${
                yearTab === "pressing"
                  ? "bg-zinc-800 text-white dark:bg-zinc-100 dark:text-zinc-900"
                  : "text-zinc-500 hover:text-zinc-700 dark:hover:text-zinc-300"
              }`}
              onClick={() => setYearTab("pressing")}
            >
              Pressing year
            </button>
            <button
              className={`px-3 py-1.5 rounded-r-md border-l border-zinc-200 transition-colors dark:border-white/10 ${
                yearTab === "original"
                  ? "bg-zinc-800 text-white dark:bg-zinc-100 dark:text-zinc-900"
                  : "text-zinc-500 hover:text-zinc-700 dark:hover:text-zinc-300"
              }`}
              onClick={() => setYearTab("original")}
            >
              Original year
            </button>
          </div>
        </div>
        {yearTab === "pressing" ? (
          <YearBarChart data={data.by_year} label="Pressing year" />
        ) : data.by_original_year.length ? (
          <YearBarChart data={data.by_original_year} label="Original year" />
        ) : (
          <div className="text-sm text-zinc-500">
            Original years are fetched when you view individual record pages. Browse your collection to populate this chart.
          </div>
        )}
      </section>

      {/* ── By artist ── */}
      <section className="rounded-lg border border-zinc-200 bg-white p-4 dark:border-white/10 dark:bg-white/[0.04]">
        <h2 className="mb-4 text-sm font-semibold">Records by artist</h2>
        <CountList data={artistData} emptyMsg="No records in collection." />
      </section>

      {/* ── By label ── */}
      <section className="rounded-lg border border-zinc-200 bg-white p-4 dark:border-white/10 dark:bg-white/[0.04]">
        <h2 className="mb-4 text-sm font-semibold">Records by label</h2>
        <CountList data={labelData} emptyMsg="No records in collection." />
      </section>
    </>
  );
}

export function ReportsPage() {
  const [period, setPeriod] = useState<"week" | "month">("week");
  const [activeTab, setActiveTab] = useState<"activity" | "collection" | "neglected">("activity");

  const report = useQuery({
    queryKey: ["reports", period],
    queryFn: () => api.reports(period),
  });

  const pickNeglected = useMutation({
    mutationFn: () => api.pickRecord({ neglected: true }),
    onSuccess: (a) => navigate(`/records/${a.id}`),
  });

  const collectionReport = useQuery({
    queryKey: ["collection-report"],
    queryFn: () => api.collectionReport(),
    enabled: activeTab === "collection",
  });

  const data = report.data;

  return (
    <div className="mt-6 space-y-6">
      {/* ── Tab switcher ── */}
      <div className="flex gap-1 rounded-lg border border-zinc-200 bg-zinc-50 p-1 dark:border-white/10 dark:bg-white/[0.03] w-fit">
        <button
          className={`px-4 py-1.5 rounded-md text-sm font-medium transition-colors ${
            activeTab === "activity"
              ? "bg-white shadow-sm dark:bg-white/[0.08] text-zinc-900 dark:text-white"
              : "text-zinc-500 hover:text-zinc-700 dark:hover:text-zinc-300"
          }`}
          onClick={() => setActiveTab("activity")}
        >
          Activity
        </button>
        <button
          className={`px-4 py-1.5 rounded-md text-sm font-medium transition-colors ${
            activeTab === "collection"
              ? "bg-white shadow-sm dark:bg-white/[0.08] text-zinc-900 dark:text-white"
              : "text-zinc-500 hover:text-zinc-700 dark:hover:text-zinc-300"
          }`}
          onClick={() => setActiveTab("collection")}
        >
          Collection
        </button>
        <button
          className={`px-4 py-1.5 rounded-md text-sm font-medium transition-colors ${
            activeTab === "neglected"
              ? "bg-white shadow-sm dark:bg-white/[0.08] text-zinc-900 dark:text-white"
              : "text-zinc-500 hover:text-zinc-700 dark:hover:text-zinc-300"
          }`}
          onClick={() => setActiveTab("neglected")}
        >
          Not played in 6+ months
          {report.data ? <span className="ml-1.5 text-xs font-normal opacity-60">({report.data.neglected.length})</span> : null}
        </button>
      </div>

      {report.isError ? (
        <div className="text-sm text-red-400">{String(report.error)}</div>
      ) : null}

      {activeTab === "collection" ? (
        collectionReport.isLoading ? (
          <div className="space-y-3">
            {[...Array(4)].map((_, i) => (
              <div key={i} className="h-32 animate-pulse rounded-lg bg-zinc-100 dark:bg-white/[0.04]" />
            ))}
          </div>
        ) : collectionReport.isError ? (
          <div className="text-sm text-red-400">{String(collectionReport.error)}</div>
        ) : collectionReport.data ? (
          <CollectionReportSections data={collectionReport.data} />
        ) : null
      ) : null}

      {/* ── Not played in 6+ months tab ── */}
      {activeTab === "neglected" && (
        <section className="rounded-lg border border-zinc-200 bg-white p-4 dark:border-white/10 dark:bg-white/[0.04]">
          <div className="mb-4 flex items-center gap-4">
            <h2 className="text-sm font-semibold">Not played in 6+ months</h2>
            {!!report.data?.neglected.length && (
              <button
                className="ml-auto text-xs font-medium px-2.5 py-1 rounded-md bg-red-500 text-white hover:bg-red-600 disabled:opacity-50 transition-colors"
                onClick={() => pickNeglected.mutate()}
                disabled={pickNeglected.isPending}
              >
                {pickNeglected.isPending ? "Picking…" : "Pick one"}
              </button>
            )}
          </div>
          {report.isLoading ? (
            <div className="space-y-2">
              {[...Array(5)].map((_, i) => (
                <div key={i} className="h-10 animate-pulse rounded bg-zinc-100 dark:bg-white/[0.04]" />
              ))}
            </div>
          ) : !data?.neglected.length ? (
            <div className="text-sm text-zinc-500">All played records have been spun in the last 6 months.</div>
          ) : (
            <ul className="max-h-[70vh] overflow-auto">
              {data.neglected.map((r) => (
                <RecordRow
                  key={r.id}
                  id={r.id}
                  artist={r.artist}
                  title={r.title}
                  year={r.year}
                  thumbUrl={r.thumb_url}
                  lastSpunAt={r.last_spun_at}
                  spinCount={r.spin_count}
                />
              ))}
            </ul>
          )}
        </section>
      )}

      {activeTab !== "activity" ? null : (
      <>
      {/* ── Collection at a glance ── */}
      <section>
        <h2 className="mb-3 text-sm font-semibold uppercase tracking-wider text-zinc-400 dark:text-zinc-500">
          Collection at a glance
        </h2>
        <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
          <StatBox
            label="Total records"
            value={data?.stats.total_records ?? "—"}
          />
          <StatBox
            label="Played"
            value={data?.stats.played_records ?? "—"}
            sub="at least once"
          />
          <StatBox
            label="Never played"
            value={data?.stats.never_played ?? "—"}
            accent={!!data && data.stats.never_played > 0}
          />
          <StatBox
            label="Utilization"
            value={data ? `${Math.round(data.stats.utilization_pct)}%` : "—"}
            sub="of collection played"
          />
        </div>
      </section>

      {/* ── Spins over time ── */}
      <section className="rounded-lg border border-zinc-200 bg-white p-4 dark:border-white/10 dark:bg-white/[0.04]">
        <div className="mb-4 flex items-center justify-between gap-2">
          <h2 className="text-sm font-semibold">Spins over time</h2>
          <div className="flex rounded-md border border-zinc-200 text-xs dark:border-white/10">
            <button
              className={`px-3 py-1.5 rounded-l-md transition-colors ${
                period === "week"
                  ? "bg-zinc-800 text-white dark:bg-zinc-100 dark:text-zinc-900"
                  : "text-zinc-500 hover:text-zinc-700 dark:hover:text-zinc-300"
              }`}
              onClick={() => setPeriod("week")}
            >
              Weekly
            </button>
            <button
              className={`px-3 py-1.5 rounded-r-md border-l border-zinc-200 transition-colors dark:border-white/10 ${
                period === "month"
                  ? "bg-zinc-800 text-white dark:bg-zinc-100 dark:text-zinc-900"
                  : "text-zinc-500 hover:text-zinc-700 dark:hover:text-zinc-300"
              }`}
              onClick={() => setPeriod("month")}
            >
              Monthly
            </button>
          </div>
        </div>
        {report.isLoading ? (
          <div className="h-32 animate-pulse rounded bg-zinc-100 dark:bg-white/[0.04]" />
        ) : (
          <SpinsBarChart data={data?.spins_over_time ?? []} period={period} />
        )}
      </section>

      {/* ── Top artists ── */}
      <section className="rounded-lg border border-zinc-200 bg-white p-4 dark:border-white/10 dark:bg-white/[0.04]">
        <h2 className="mb-4 text-sm font-semibold">Most-played artists</h2>
        {report.isLoading ? (
          <div className="space-y-2">
            {[...Array(5)].map((_, i) => (
              <div key={i} className="h-6 animate-pulse rounded bg-zinc-100 dark:bg-white/[0.04]" />
            ))}
          </div>
        ) : !data?.top_artists.length ? (
          <div className="text-sm text-zinc-500">No spins recorded yet.</div>
        ) : (
          <ul className="space-y-2">
            {data.top_artists.map((a, i) => {
              const maxSpins = data.top_artists[0]?.spin_count ?? 1;
              const barPct = maxSpins > 0 ? (a.spin_count / maxSpins) * 100 : 0;
              return (
                <li key={a.artist} className="flex items-center gap-3">
                  <div className="w-5 shrink-0 text-right text-xs text-zinc-400">{i + 1}</div>
                  <div className="min-w-0 flex-1">
                    <div className="mb-1 flex items-baseline justify-between gap-2">
                      <span className="truncate text-sm font-medium">{a.artist}</span>
                      <span className="shrink-0 text-xs text-zinc-400">
                        {a.spin_count} {a.spin_count === 1 ? "spin" : "spins"} · {a.record_count} {a.record_count === 1 ? "record" : "records"}
                      </span>
                    </div>
                    <div className="h-1.5 w-full overflow-hidden rounded-full bg-zinc-100 dark:bg-white/[0.06]">
                      <div
                        className="h-full rounded-full bg-red-500"
                        style={{ width: `${barPct}%` }}
                      />
                    </div>
                  </div>
                </li>
              );
            })}
          </ul>
        )}
      </section>

      {/* ── Never played / Neglected (moved: "Not played in 6+ months" is now its own top-level tab; "Never played" hidden for now) ──
      <section className="rounded-lg border border-zinc-200 bg-white p-4 dark:border-white/10 dark:bg-white/[0.04]">
        <div className="mb-4 flex items-center gap-4">
          <button ... Never played tab ... />
          <button ... Not played in 6+ months tab ... />
        </div>
        ... list content ...
      </section>
      ── */}
      </>
      )}
    </div>
  );
}
