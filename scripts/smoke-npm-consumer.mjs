import assert from 'node:assert/strict';
import {openStow,serveWorkspace,prepareWorkspace} from '@chester-hill-solutions/stow-s3';
import {mkdtemp,writeFile,readFile,rm} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
const session=await openStow();
try {
 assert.ok(session.endpoint.startsWith('http://127.0.0.1:'));
 assert.ok(process.argv[2], 'expected release version is required');
 assert.equal(session.capabilities().binaryVersion,process.argv[2]);
}
finally { await session.close(); }
const root=await mkdtemp(join(tmpdir(),'stow-consumer-'));
try {
 await writeFile(join(root,'input.txt'),'portable');
 await writeFile(join(root,'task.json'),JSON.stringify({version:1,root:join(root,'workspace'),registry_dir:join(root,'registry'),inputs:[{source:join(root,'input.txt'),destination:'input.txt'}]}));
 const prepared=await prepareWorkspace(join(root,'task.json'));
 const serving=await serveWorkspace({id:prepared.workspace_id,registryDir:prepared.registry_dir});
 await serving.close();
 assert.equal(await readFile(join(root,'workspace','input.txt'),'utf8'),'portable');
} finally { await rm(root,{recursive:true,force:true}); }
console.log('Clean npm consumer session and persistent workspace passed');
