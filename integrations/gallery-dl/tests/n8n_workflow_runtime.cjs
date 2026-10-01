// Run inside the staged n8n image with only private workflow copies mounted.
// Evaluates parameters and checkpoints the real Wait node; no commands run.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const { createRequire } = require('node:module');
const n8nRequire = createRequire(fs.realpathSync('/usr/local/bin/n8n'));
const { Expression } = n8nRequire('n8n-workflow');
const base = path.dirname(n8nRequire.resolve('n8n-nodes-base'));
const { Wait } = require(path.join(base, 'dist/nodes/Wait/Wait.node.js'));

async function main() {
  const original = JSON.parse(fs.readFileSync(process.argv[2], 'utf8'));
  const staged = JSON.parse(fs.readFileSync(process.argv[3], 'utf8'));
  assert.equal(staged.length, original.length);
  const expression = new Expression('UTC');
  let evaluations = 0;
  function evaluate(value, json, workflow) {
    evaluations++;
    return expression.resolveSimpleParameterValue(value, {
      $json: structuredClone(json), $execution: { id: '12345' },
      $itemIndex: 3, $workflow: { id: workflow.id }, $thisRunIndex: 0, $thisItemIndex: 3,
    });
  }
  for (let index = 0; index < staged.length; index++) {
    const before = original[index], workflow = staged[index];
    for (const key of Object.keys(before)) {
      if (!['nodes', 'connections'].includes(key)) assert.deepEqual(workflow[key], before[key], `retained ${key}`);
    }
    const nodes = new Map(workflow.nodes.map(node => [node.name, node]));
    for (const old of before.nodes) {
      const node = nodes.get(old.name);
      assert.equal(node.id, old.id);
      assert.deepEqual(node.credentials, old.credentials);
    }
    const commands = workflow.nodes.filter(node => node.type === 'n8n-nodes-base.executeCommand');
    const start = commands.find(node => node.parameters.command.includes(' --mode '));
    const inspect = commands.find(node => node.parameters.command.includes(' --inspect='));
    const json = { username: 'Native_Fixture', userId: '123456789' };
    const rendered = evaluate(start.parameters.command, json, workflow);
    assert.match(rendered, /^\/opt\/stash-ingest\/bin\/stash-ingest-n8n --mode /);
    assert.ok(!rendered.includes('{{'));
    assert.ok(rendered.includes(`--workflow='${workflow.id}' --execution='12345' --node='${start.id}' --item='3'`));
    assert.equal(evaluate(start.parameters.command, json, workflow), rendered);
    const rejected = evaluate(start.parameters.command, { username: "a'; false #", userId: "1'; false #" }, workflow);
    assert.ok(rejected.includes("--identity='INVALID!'"));
    const token = '0123456789abcdef0123456789abcdef';
    const initial = evaluate(inspect.parameters.command, { stdout: JSON.stringify({ token }) }, workflow);
    assert.equal(initial, evaluate(inspect.parameters.command, { token }, workflow));
    assert.equal(initial, `/opt/stash-ingest/bin/stash-ingest-n8n --inspect='${token}'`);
    assert.ok(evaluate(inspect.parameters.command, { token: "'; false #" }, workflow).endsWith("--inspect='INVALID!'"));
    const parseName = workflow.connections[inspect.name].main[0][0].node;
    const parse = nodes.get(parseName);
    const pending = nodes.get('If native backfill pending');
    for (const isPending of [true, false]) {
      const response = { token, backfill_pending: isPending, command_failed: false, exit_code: isPending ? 2 : 0,
        network_blocked: false, stderr_tail: '', stdout_tail: isPending ? 'pending' : 'completed' };
      const parsed = {};
      for (const assignment of parse.parameters.assignments.assignments) {
        parsed[assignment.name] = evaluate(assignment.value, { stdout: JSON.stringify(response) }, workflow);
      }
      assert.equal(parsed.token, token);
      assert.equal(evaluate(pending.parameters.conditions.conditions[0].leftValue, parsed, workflow), isPending);
      const branch = workflow.connections[pending.name].main[isPending ? 0 : 1][0].node;
      assert.equal(branch === 'Wait for native backfill', isPending);
    }
    const wait = nodes.get('Wait for native backfill');
    let checkpoint;
    const input = [{ json: { token, backfill_pending: true } }];
    const started = Date.now();
    const output = await new Wait().execute({
      getNodeParameter: name => wait.parameters[name], getInputData: () => input,
      putExecutionToWait: async until => { checkpoint = until; },
      onExecutionCancellation: cancel => { cancel(); throw new Error('Wait did not persist a checkpoint'); },
    });
    assert.ok(checkpoint instanceof Date);
    assert.ok(checkpoint.getTime() - started >= 89000 && checkpoint.getTime() - started <= 91000);
    assert.deepEqual(output, [input]);
    assert.equal(workflow.connections[wait.name].main[0][0].node, inspect.name);
  }
  console.log(JSON.stringify({ workflows: staged.length, evaluations, wait_checkpoints: staged.length,
    node_ids_and_credentials_preserved: true, commands_executed: 0 }));
}

main().catch(error => { console.error('n8n runtime validation failed:', error.code || error.name); process.exitCode = 1; });
