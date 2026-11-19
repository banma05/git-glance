// BranchName writes a branch name so that a narrow row cuts it from the front:
// the folders before the last "/" shrink first; when the row text is narrow
// they are dropped whole for a "…" (app.css .bname, @container rowtext); the
// last segment (with its "/") stays whole while it fits, then is cut at its
// end. CSS does the cutting. What is shown is hidden from assistive technology,
// which gets the whole name (`full`, the shown name by default) instead.
export function BranchName({ name, full = name, class: cls = '' }: { name: string; full?: string; class?: string }) {
  const cut = name.lastIndexOf('/');
  const prefix = cut > 0 ? name.slice(0, cut) : '';
  const last = cut > 0 ? name.slice(cut) : name;
  return (
    <span class={cls ? `bname ${cls}` : 'bname'}>
      {prefix && (
        <span class="bname-pre" aria-hidden="true">
          <bdi>{prefix}</bdi>
        </span>
      )}
      <span class="bname-last" aria-hidden="true">
        {last}
      </span>
      <span class="vh">{full}</span>
    </span>
  );
}
