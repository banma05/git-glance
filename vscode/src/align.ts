import * as vscode from 'vscode';
import type { CoreClient } from './core';

interface AlignReport {
  fetch?: { ok: boolean; error: string };
  updated?: unknown[];
  skipped?: unknown[];
}

// One repository's part of "align all": its report, or why the call failed.
export interface AlignOutcome {
  repo: string;
  report: AlignReport | null;
  error: string; // "" when the core answered
}

const baseName = (p: string) => p.split(/[\\/]+/).filter(Boolean).pop() ?? p;

// alignSummary is the one line shown after "align all": what moved, what was
// left alone, and every repository that failed or could not read its remote.
export function alignSummary(outcomes: AlignOutcome[]): { text: string; problems: boolean } {
  let updated = 0;
  let skipped = 0;
  const failed: string[] = [];
  const offline: string[] = [];
  for (const o of outcomes) {
    if (!o.report) {
      failed.push(`${baseName(o.repo)} (${o.error})`);
      continue;
    }
    updated += o.report.updated?.length ?? 0;
    skipped += o.report.skipped?.length ?? 0;
    if (o.report.fetch && !o.report.fetch.ok) offline.push(`${baseName(o.repo)} (${o.report.fetch.error})`);
  }
  const parts = [vscode.l10n.t('Git Glance: {0} branches updated, {1} left alone.', updated, skipped)];
  if (failed.length) parts.push(vscode.l10n.t('Not aligned: {0}.', failed.join(', ')));
  if (offline.length) parts.push(vscode.l10n.t('Could not read the remote of {0}; their branches were aligned to what had been fetched before.', offline.join(', ')));
  return { text: parts.join(' '), problems: failed.length > 0 || offline.length > 0 };
}

// alignAll fast-forwards the branches of every listed repository, after the
// user confirms. The core only moves branches that are clean and only behind.
// done runs once the repositories were aligned, so the pages read them again.
export async function alignAll(core: Pick<CoreClient, 'call'>, log: (l: string) => void, done: () => void): Promise<void> {
  const yes = vscode.l10n.t('Align');
  const pick = await vscode.window.showInformationMessage(
    vscode.l10n.t('Fast-forward every branch that is only behind its remote in all listed repositories?'),
    { modal: true, detail: vscode.l10n.t('Branches with local changes or commits of their own are left alone.') },
    yes,
  );
  if (pick !== yes) return;
  let repos: string[];
  try {
    repos = await core.call<string[]>('repos');
  } catch (e: any) {
    void vscode.window.showErrorMessage(vscode.l10n.t('Git Glance: {0}', String(e?.message ?? e)));
    return;
  }
  const outcomes: AlignOutcome[] = [];
  for (const repo of repos) {
    try {
      outcomes.push({ repo, report: await core.call<AlignReport>('align', { repo }), error: '' });
    } catch (e: any) {
      log(`align failed for ${repo}: ${e?.message ?? e}`);
      outcomes.push({ repo, report: null, error: String(e?.message ?? e) });
    }
  }
  done();
  const { text, problems } = alignSummary(outcomes);
  if (problems) void vscode.window.showWarningMessage(text);
  else void vscode.window.showInformationMessage(text);
}
