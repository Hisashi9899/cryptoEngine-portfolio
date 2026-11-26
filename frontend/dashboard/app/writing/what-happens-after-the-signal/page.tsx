import type { Metadata } from "next";

const SITE_URL = "https://hisashi9899.github.io/cryptoEngine-portfolio/";
const ARTICLE_URL = `${SITE_URL}writing/what-happens-after-the-signal/`;
const AUTHOR_URL = "https://contra.com/neutron_d0wbklm2/work";

const TITLE = "What happens after the signal";
const DESCRIPTION =
  "A trading bot is a signal generator. A trading system is everything that has to be true when the signal fires: state, retries, cost gates, risk limits and an audit trail.";

export const metadata: Metadata = {
  title: `${TITLE} — CryptoEngine`,
  description: DESCRIPTION,
  alternates: { canonical: ARTICLE_URL },
  authors: [{ name: "Hisashi Aoki", url: AUTHOR_URL }],
  openGraph: {
    title: TITLE,
    description: DESCRIPTION,
    url: ARTICLE_URL,
    siteName: "CryptoEngine",
    type: "article",
    locale: "en_US",
  },
  twitter: { card: "summary", title: TITLE, description: DESCRIPTION },
};

const STRUCTURED_DATA = {
  "@context": "https://schema.org",
  "@type": "TechArticle",
  headline: TITLE,
  description: DESCRIPTION,
  url: ARTICLE_URL,
  datePublished: "2026-11-26",
  author: {
    "@type": "Person",
    name: "Hisashi Aoki",
    url: AUTHOR_URL,
    jobTitle: "Full-Spectrum Product Engineer",
  },
  about: ["trading systems", "distributed state", "risk controls", "reliability engineering"],
  isPartOf: { "@type": "WebSite", url: SITE_URL, name: "CryptoEngine" },
};

export default function Article() {
  return (
    <div className="min-h-screen bg-navy-950 text-slate-200">
      <script
        type="application/ld+json"
        dangerouslySetInnerHTML={{ __html: JSON.stringify(STRUCTURED_DATA) }}
      />

      <header className="mx-auto flex max-w-3xl items-center justify-between px-6 py-6">
        <a href={SITE_URL} className="text-sm text-slate-400 transition-colors hover:text-slate-100">
          ← CryptoEngine
        </a>
        <a
          href={AUTHOR_URL}
          className="rounded-lg border border-emerald-400/30 bg-emerald-400/10 px-3 py-1.5 text-xs font-medium text-emerald-200 transition-colors hover:bg-emerald-400/20"
        >
          Hire me on Contra
        </a>
      </header>

      <article className="mx-auto max-w-3xl px-6 pb-20">
        <p className="text-[11px] uppercase tracking-[0.18em] text-emerald-300">Engineering note</p>
        <h1 className="mt-3 text-3xl font-bold leading-tight tracking-tight text-white md:text-4xl">
          {TITLE}
        </h1>
        <p className="mt-4 text-lg leading-relaxed text-slate-300">{DESCRIPTION}</p>
        <p className="mt-3 text-xs text-slate-500">Hisashi Aoki · 26 November 2026 · 7 min read</p>

        <div className="mt-10 space-y-6 text-[15px] leading-relaxed text-slate-300">
          <p>
            A trading bot is a signal generator. A trading system is everything that must stay true
            when the signal fires: state, failure semantics, cost, risk, and evidence. Most public
            repositories stop at the first part, which is why they look impressive and behave badly
            the moment they meet a real exchange.
          </p>

          <h2 className="pt-4 text-xl font-semibold text-white">Restart with an open position</h2>
          <p>
            The most common production failure is not a bad strategy. It is a process that restarts
            holding a position it no longer remembers. If the engine re-enters on startup, it doubles
            exposure; if it forgets the position, it never exits.
          </p>
          <p>
            CryptoEngine makes one goroutine the only writer of mutable state. Positions, pending
            maker orders and daily P&amp;L live behind that coordinator, persist as atomic snapshots,
            and reconcile on restart before any new decision is allowed. Workers consume order-book
            streams and trade tape and forward immutable snapshots; they never mutate state
            directly. Decisions are made on closed bars, so throughput stays bounded and every
            decision sees a consistent world.
          </p>

          <h2 className="pt-4 text-xl font-semibold text-white">The exchange times out mid-order</h2>
          <p>
            Networks fail between &quot;sent&quot; and &quot;acknowledged&quot;. The question is
            whether the retry creates a duplicate order.
          </p>
          <p>
            The executor separates retryable faults (timeouts, transient 5xx) from business
            rejections, which fail fast and are never retried. Retries use bounded backoff. Before
            the request, the engine checks maintenance windows and lot-size rules, so a retry cannot
            silently create a second position. Anything ambiguous is treated as unknown rather than
            assumed successful.
          </p>

          <h2 className="pt-4 text-xl font-semibold text-white">Fees and spread are part of the strategy</h2>
          <p>
            A signal can be directionally correct and still lose money after fees and spread. The
            cost gate blocks entries unless expected movement covers both. If volatility (ATR) is
            unknown, the gate <strong>fails closed</strong>: no trade, rather than a guess. This is
            the least popular rule in the system and the one that keeps the equity curve honest.
          </p>

          <h2 className="pt-4 text-xl font-semibold text-white">Risk is layered, not a single check</h2>
          <ul className="list-disc space-y-2 pl-5">
            <li>per-symbol position limits and portfolio caps;</li>
            <li>a daily loss kill switch that stops the engine, not just the next order;</li>
            <li>stop-loss and maximum holding time per position;</li>
            <li>correlation clustering, so three &quot;independent&quot; positions are not one bet;</li>
            <li>maker/taker selection that accounts for the fill probability, not only the fee.</li>
          </ul>
          <p>
            Each layer is boring on its own. The value is that they are evaluated together before
            any order reaches the exchange.
          </p>

          <h2 className="pt-4 text-xl font-semibold text-white">Audit first, dashboards second</h2>
          <p>
            Every signal, skip, order, fill and state transition is appended to a log. That log is
            what makes the dashboard trustworthy: the UI is a view over recorded decisions, not a
            separate computation that can disagree with the engine. It also makes incident review
            possible — you can reconstruct why a trade happened, including the reasons that blocked
            other trades.
          </p>

          <h2 className="pt-4 text-xl font-semibold text-white">How do you know a change is real?</h2>
          <p>
            Strategy changes usually look good on the sample that motivated them. The research loop
            exists to make that harder: historical replay on recorded data, bootstrap confidence
            intervals on profit factor, explicit acceptance gates, and ablation studies to check
            whether a component actually contributes. A change that cannot clear the gate does not
            go near capital, regardless of how good the backtest looks.
          </p>

          <h2 className="pt-4 text-xl font-semibold text-white">24/7 on a 2 GB VPS</h2>
          <p>
            Reliability on small infrastructure is mostly about refusing to fail silently. A
            heartbeat is written on every loop; a watchdog alerts when it stops; resource limits keep
            a leak from taking down the host; the deploy script refuses to ship secrets. The system
            is designed to keep running when the database or the exchange is briefly unavailable,
            and to say so when it cannot.
          </p>

          <h2 className="pt-4 text-xl font-semibold text-white">What I would do differently</h2>
          <p>
            I would model the lifecycle of an order as an explicit state machine in the domain layer,
            rather than inferring it from records. It would make partial fills, cancels and exchange
            rejections easier to test as transitions, and it would remove the one place where the
            current implementation relies on carefully written procedural code instead of an
            invariant the type system can check.
          </p>

          <h2 className="pt-4 text-xl font-semibold text-white">The point</h2>
          <p>
            Deterministic state, cost-aware execution, layered risk, an audit trail, a research loop
            and low-footprint operations are one design problem, not six features. That is the
            difference between a strategy script with a dashboard bolted on and a system you can
            leave running.
          </p>
        </div>

        <div className="mt-12 rounded-2xl border border-white/10 bg-white/[0.03] p-6">
          <p className="text-[11px] uppercase tracking-[0.16em] text-emerald-300">Work with me</p>
          <p className="mt-2 text-sm leading-relaxed text-slate-300">
            I build production systems end to end — event-driven backends, APIs, CI/CD and the
            dashboards on top. Healthcare, trading and audit-heavy domains are the specialty.
          </p>
          <div className="mt-4 flex flex-wrap gap-3">
            <a
              href={AUTHOR_URL}
              className="rounded-xl bg-emerald-500 px-4 py-2.5 text-sm font-semibold text-navy-950 transition-colors hover:bg-emerald-400"
            >
              Hire me on Contra
            </a>
            <a
              href={SITE_URL}
              className="rounded-xl border border-white/15 bg-white/5 px-4 py-2.5 text-sm font-semibold transition-colors hover:bg-white/10"
            >
              Open the demo
            </a>
          </div>
        </div>
      </article>
    </div>
  );
}
