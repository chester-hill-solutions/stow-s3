import assert from "node:assert/strict";
import {test} from "node:test";
import {releasePlan} from "./release-plan.mjs";

const candidate={version:"v0.3.0",packageVersion:"0.3.0",publishPackages:false,notes:"Release notes"};
test("downloads-only policy explicitly disables registry publication",()=>{
  assert.deepEqual(releasePlan(candidate),{version:"0.3.0",publishPackages:false,notesFile:"docs/releases/v0.3.0.md"});
});
test("registry publication requires an explicit policy choice",()=>{
  assert.equal(releasePlan({...candidate,publishPackages:true}).publishPackages,true);
  for(const publishPackages of [undefined,null,"false",0]) assert.throws(()=>releasePlan({...candidate,publishPackages}),/explicitly choose/);
});
test("wrong tags and missing release notes cannot reach publication",()=>{
  assert.throws(()=>releasePlan({...candidate,version:"v0.4.0"}),/version differ/);
  assert.throws(()=>releasePlan({...candidate,version:"../main"}),/semver/);
  assert.throws(()=>releasePlan({...candidate,notes:" "}),/notes must exist/);
});
