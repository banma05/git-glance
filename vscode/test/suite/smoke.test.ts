import * as assert from 'node:assert';
import * as vscode from 'vscode';

suite('Git Glance smoke', () => {
  test('activates, shows the view and the core reports git', async function () {
    this.timeout(60_000);
    const ext = vscode.extensions.getExtension('banma05.git-glance');
    assert.ok(ext, 'extension is installed in the test host');
    const api = await ext.activate();
    assert.ok(ext.isActive);
    await vscode.commands.executeCommand('workbench.view.extension.gitglance');
    const v = await api.core().call('version');
    assert.strictEqual(v.gitOK, true);
    assert.ok(api.CoreClient);
  });
});
