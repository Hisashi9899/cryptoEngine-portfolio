import type { Metadata } from "next";

const SITE_URL = "https://hisashi9899.github.io/cryptoEngine-portfolio/";
const ARTICLE_URL = `${SITE_URL}writing/evaluation-is-an-audit-trail/`;
const AUTHOR_URL = "https://contra.com/neutron_d0wbklm2/work";
const SERVICE_URL = "https://contra.com/s/cMMml7Pl-ai-evaluation-and-audit-readiness";

const TITLE = "Evaluation is an audit trail, not a leaderboard";
const DESCRIPTION =
  "AI quality needs the same evidence discipline as financial controls: rubrics with owners, versioned datasets, per-release regression gates, red-team cases stored as tests, and retests.";

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
  about: ["AI evaluation", "LLM red teaming", "regression testing", "AI governance"],
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
            A leaderboard answers “which model scores higher on someone else&apos;s questions?”. An audit
            answers “how do you know this system behaved correctly on our work, and what happens when
            it does not?”. Most teams have the first and need the second.
          </p>

          <h2 className="pt-4 text-xl font-semibold text-white">
            Three failures ordinary tests miss
          </h2>
          <ul className="list-disc space-y-2 pl-5">
            <li>
              <strong>Confident wrong answers.</strong> The output is well-formed, fluent and
              incorrect. Unit tests pass because nothing crashed.
            </li>
            <li>
              <strong>Silent regressions.</strong> A prompt tweak fixes one case and quietly breaks
              three others that nobody re-ran.
            </li>
            <li>
              <strong>Unsafe actions.</strong> The model calls a tool with the wrong arguments, or
              with the right arguments for the wrong user.
            </li>
          </ul>
          <p>
            None of these appear in a demo. They appear in production, usually in the least
            instrumented workflow.
          </p>

          <h2 className="pt-4 text-xl font-semibold text-white">Rubric first, tooling second</h2>
          <p>
            Before choosing an evaluation library, write down what “good” means per use case and who
            owns the definition: accuracy, groundedness, task completion, privacy, safety, and
            escalation. Each needs a threshold and a consequence — a rubric without a consequence is
            documentation, not a control.
          </p>
          <p>
            This is the part teams skip, and it is the only part an auditor actually reads. If two
            reviewers cannot apply the rubric to the same output and agree, the metric is not
            measuring quality; it is measuring taste.
          </p>

          <h2 className="pt-4 text-xl font-semibold text-white">Datasets are a product</h2>
          <p>
            An evaluation set is not a CSV someone exported once. It needs provenance, versioning,
            coverage of the workflows that matter, and a rule for how new cases enter it. Otherwise
            it drifts away from production while everyone keeps reporting green.
          </p>
          <ul className="list-disc space-y-2 pl-5">
            <li>sample from real traffic shapes, even when the data itself must be synthetic;</li>
            <li>keep a small “golden set” reviewed by domain experts, and change it deliberately;</li>
            <li>record expected behaviour, not just expected text;</li>
            <li>version the set alongside prompts and models.</li>
          </ul>

          <h2 className="pt-4 text-xl font-semibold text-white">
            The harness: make regressions impossible to miss
          </h2>
          <p>
            The useful unit of comparison is a release, not a prompt. Every candidate change runs
            against the same versioned dataset, and the report shows deltas per metric with the
            sample size behind them. A two-point improvement on forty cases is noise; treating it as
            a win is how quality erodes.
          </p>
          <p>
            Gates should fail closed: a change that drops safety or escalation below threshold does
            not ship because it improved helpfulness. That is the same discipline as a cost gate in a
            trading system — if the input is unknown, the safe answer is no.
          </p>

          <h2 className="pt-4 text-xl font-semibold text-white">Red-team cases belong in the test suite</h2>
          <p>
            Prompt injection, jailbreaks, data leakage and unsafe tool use are usually handled as
            one-off exercises: a PDF is produced, findings are listed, everyone moves on. The finding
            should become a test case with an expected behaviour, stored with the rest of the
            evaluation set and re-run on every change.
          </p>
          <p>
            That converts a point-in-time assessment into evidence. “We tested it in March” is not a
            control. “This case has been re-run on every release since March, and here is the result
            for this build” is.
          </p>

          <h2 className="pt-4 text-xl font-semibold text-white">Reporting that survives review</h2>
          <p>
            Findings need an owner, a severity, a remediation and a retest date. The monthly report
            should fit on two pages: what changed, what broke, what was fixed, what is still open,
            and which thresholds were missed. Procurement and security teams ask for exactly this,
            and they can tell the difference between a summary and a document that was assembled the
            week before the meeting.
          </p>

          <h2 className="pt-4 text-xl font-semibold text-white">What I would do differently</h2>
          <p>
            I would treat the evaluation set as a product from day one: an owner, a changelog, and a
            small budget for curating new cases every month. The most common failure I have seen is
            not a weak model — it is an evaluation set that stopped representing the work six months
            ago while the dashboard stayed green.
          </p>

          <h2 className="pt-4 text-xl font-semibold text-white">The point</h2>
          <p>
            Evaluation is not a score you put in a slide. It is the evidence that a system behaves as
            intended, on your work, at this version, with the failures documented. That is an audit
            trail, and it is built the same way as any other: define the control, test it, record the
            result, retest after every change.
          </p>
        </div>

        <div className="mt-12 rounded-2xl border border-white/10 bg-white/[0.03] p-6">
          <p className="text-[11px] uppercase tracking-[0.16em] text-emerald-300">Work with me</p>
          <p className="mt-2 text-sm leading-relaxed text-slate-300">
            I set up AI evaluation and audit readiness programmes: rubrics, versioned datasets,
            regression gates and red-team cases stored as tests. Two-week scoped assessment to start.
          </p>
          <div className="mt-4 flex flex-wrap gap-3">
            <a
              href={SERVICE_URL}
              className="rounded-xl bg-emerald-500 px-4 py-2.5 text-sm font-semibold text-navy-950 transition-colors hover:bg-emerald-400"
            >
              AI Evaluation &amp; Audit Readiness
            </a>
            <a
              href={AUTHOR_URL}
              className="rounded-xl border border-white/15 bg-white/5 px-4 py-2.5 text-sm font-semibold transition-colors hover:bg-white/10"
            >
              Hire me on Contra
            </a>
          </div>
        </div>
      </article>
    </div>
  );
}
