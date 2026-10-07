// Run in the isolated current n8n image with private graph copies mounted.
// No source mutation or scraper command is executed.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const { createRequire } = require('node:module');
const n8nRequire = createRequire(fs.realpathSync('/usr/local/bin/n8n'));
const { Expression } = n8nRequire('n8n-workflow');
const base = path.dirname(n8nRequire.resolve('n8n-nodes-base'));
const { ExecuteCommand } = require(path.join(base, 'dist/nodes/ExecuteCommand/ExecuteCommand.node.js'));

async function main() {
  const original = JSON.parse(fs.readFileSync(process.argv[2]));
  const staged = JSON.parse(fs.readFileSync(process.argv[3]));
  const expression = new Expression('UTC');
  let workflows = 0, evaluations = 0, normalizations = 0, resultChecks = 0;
  const execute = new ExecuteCommand();
  const commandContext = (command, items = 1) => ({
    getInputData: () => Array.from({ length: items }, () => ({ json: {} })),
    getNodeParameter: name => name === 'executeOnce' ? false : command,
    getExecutionCancelSignal: () => new AbortController().signal,
    getNode: () => ({ id: 'fixture', name: 'Fixture', type: 'n8n-nodes-base.executeCommand', typeVersion: 1, position: [0, 0] }),
    continueOnFail: () => false,
  });
  const help = await execute.execute.call(commandContext('/opt/stash-ingest/bin/stash-n8n-sources --help', 2));
  assert.equal(help[0].length, 2);
  for (const [index, item] of help[0].entries()) {
    assert.equal(item.json.exitCode, 0);
    assert.ok(item.json.stdout.includes('remove-performer'));
    assert.equal(item.pairedItem.item, index);
  }
  await assert.rejects(execute.execute.call(commandContext(
    '/opt/stash-ingest/bin/stash-n8n-sources --action add --platform reddit --identity fixture --workflow fixture --execution 1 --node 11111111-1111-4111-8111-111111111111 --item 0 --runtime /tmp/unconfigured-native-source-runtime.json --endpoint http://fixture.invalid --producer 22222222-2222-4222-8222-222222222222'
  )), error => error.name === 'NodeOperationError' && error.message.includes('Command failed'));
  assert.equal(fs.existsSync('/tmp/unconfigured-native-source-runtime.json'), false);
  for (const workflow of staged) {
    const command = workflow.nodes.find(node => node.name === 'Manage native sources');
    if (!command) continue;
    workflows++;
    const before = original.find(row => row.id === workflow.id);
    assert.ok(before);
    for (const key of Object.keys(before)) {
      if (!['nodes', 'connections'].includes(key)) assert.deepEqual(workflow[key], before[key]);
    }
    assert.equal(command.parameters.executeOnce, false);
    assert.equal(command.continueOnFail, undefined);
    assert.equal(command.onError, undefined);
    const names = new Map(workflow.nodes.map(node => [node.name, node]));
    assert.equal(names.size, workflow.nodes.length);
    assert.equal(workflow.nodes.some(node => node.type === 'n8n-nodes-base.graphql' || node.type === 'n8n-nodes-base.readWriteFile'), false);
    for (const [from, outputs] of Object.entries(workflow.connections)) {
      assert.ok(names.has(from));
      for (const branches of Object.values(outputs)) for (const branch of branches) for (const link of branch) assert.ok(names.has(link.node));
    }
    const render = (json, context = {}) => {
      evaluations++;
      return expression.resolveSimpleParameterValue(command.parameters.command, {
        $json: json, $execution: { id: '12345' }, $itemIndex: 3, ...context,
      });
    };
    const json = { username: 'Fixture_User', user: { id: '1234567890123456789' }, performerId: '42' };
    const expected = command.parameters.command.includes('--action remove-performer') ? '42'
      : command.parameters.command.includes('--platform twitter') ? json.user.id : json.username;
    const rendered = render(json);
    assert.ok(rendered.startsWith('/opt/stash-ingest/bin/stash-n8n-sources '));
    assert.ok(rendered.includes(`--identity='${expected}'`));
    assert.ok(rendered.includes(`--workflow='${workflow.id}' --execution='12345' --node='${command.id}' --item='3'`));
    assert.equal(rendered.includes('{{'), false);
    for (const bad of ["a'; false #", '$(false)', undefined, null]) {
      const renderedBad = render({ username: bad, user: { id: bad }, performerId: bad });
      assert.ok(renderedBad.includes("--identity='INVALID!'"));
    }
    assert.ok(render(json, { $execution: { id: "1'; false #" } }).includes("--execution='INVALID!'"));
    assert.ok(render(json, { $itemIndex: -1 }).includes("--item='INVALID!'"));
    if (command.parameters.command.includes('--platform twitter') && !command.parameters.command.includes('--action remove-performer')) {
      assert.ok(render({ user: { id: 1234567890123456789 } }).includes("--identity='INVALID!'"), 'reject rounded account numbers');
    }
    const normalize = names.get('Normalize Performer ID');
    if (normalize) {
      const output = vm.runInNewContext(`(function () { ${normalize.parameters.jsCode} })()`, {
        $input: { all: () => [{ json: { performerId: '42' } }, { json: { performerUrl: 'https://fixture.invalid/performers/43/scenes' } }] },
      });
      assert.deepEqual(JSON.parse(JSON.stringify(output)), [
        { json: { performerId: '42' }, pairedItem: { item: 0 } },
        { json: { performerId: '43' }, pairedItem: { item: 1 } },
      ]);
      normalizations += 2;
    }
    const resultNode = names.get('Source operation result');
    if (resultNode) {
      for (const status of ['removed', 'noop']) {
        const output = vm.runInNewContext(`(function () { ${resultNode.parameters.jsCode} })()`, {
          $input: { all: () => [{ json: { stdout: JSON.stringify({ complete: true, status, removedCount: status === 'removed' ? 1 : 0 }) } }] },
          $: () => ({ itemMatching: () => ({ json: { user: { screen_name: 'Fixture' } } }) }),
        });
        assert.equal(output[0].json.status, status);
        assert.equal(output[0].pairedItem.item, 0);
        resultChecks++;
      }
      assert.throws(() => vm.runInNewContext(`(function () { ${resultNode.parameters.jsCode} })()`, {
        $input: { all: () => [{ json: { stdout: JSON.stringify({ complete: false, status: 'removed' }) } }] },
      }));
      resultChecks++;
    } else {
      const children = workflow.nodes.filter(node => node.type === 'n8n-nodes-base.executeWorkflow');
      assert.ok(children.length > 0);
      for (const child of children) {
        assert.equal(child.parameters.options.waitForSubWorkflow, true);
        assert.deepEqual(child, before.nodes.find(node => node.id === child.id));
        assert.equal(workflow.connections[child.name].main[1][0].node, 'Stop and Error');
        assert.equal(child.continueOnFail, undefined);
        assert.notEqual(child.onError, 'continueRegularOutput');
      }
    }
  }
  assert.equal(workflows, 5);
  console.log(JSON.stringify({ source_workflows: workflows, expression_evaluations: evaluations,
    normalization_items: normalizations, result_checks: resultChecks, real_command_checks: 3,
    source_mutations: 0, scraper_commands: 0 }));
}

main().catch(error => { console.error('Native source workflow check failed:', error.code || error.name, error.message); process.exitCode = 1; });
