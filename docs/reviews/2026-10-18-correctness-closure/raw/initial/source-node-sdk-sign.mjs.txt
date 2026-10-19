import {createRequire} from 'node:module';
import {createHash,createHmac} from 'node:crypto';
import {readFile,writeFile} from 'node:fs/promises';
const root=process.env.STOW_RESULT_ROOT;
if(process.getuid()!==10001||!root?.startsWith('/results/')||process.env.RAILWAY_SERVICE_ID!=='83497842-c51d-420e-8ba1-181589730ad4')throw Error('Railway audit only');
const require=createRequire('/app/packages/stow-s3/package.json');
const {SignatureV4}=require('@smithy/signature-v4');
class Hash {constructor(secret){this.hash=secret?createHmac('sha256',secret):createHash('sha256')}update(data){this.hash.update(data)}async digest(){return this.hash.digest()}}
const credentials={accessKeyId:'AKIAEXAMPLE',secretAccessKey:'SECRETKEYEXAMPLESECRETKEYEXAMPLE12'};
const signer=new SignatureV4({credentials,region:'us-east-1',service:'s3',sha256:Hash,uriEscapePath:false});
const cases=[{name:'ascii',query:{z:'last',a:'first'},raw:'z=last&a=first'},{name:'encoded-key',query:{z:'last','é':'first'},raw:'z=last&%C3%A9=first'}];
const fixtures=[];
for(const row of cases){const request={method:'GET',protocol:'http:',hostname:'localhost',path:'/bucket/key',query:row.query,headers:{host:'localhost','x-amz-content-sha256':'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855'}};const signed=await signer.sign(request,{signingDate:new Date('2026-10-18T12:00:00Z')});fixtures.push({name:row.name,url:'http://localhost/bucket/key?'+row.raw,headers:signed.headers})}
await writeFile(root+'/node-sdk-signatures.json',JSON.stringify(fixtures,null,2));
const version=JSON.parse(await readFile('/app/packages/stow-s3/node_modules/@smithy/signature-v4/package.json','utf8')).version;
const entry=require.resolve('@smithy/signature-v4');const source=await readFile(entry);await writeFile(root+'/node-sdk-signer.js.txt',source);
await writeFile(root+'/node-sdk-receipt.json',JSON.stringify({version,entry,sourceSHA256:createHash('sha256').update(source).digest('hex'),signatures:fixtures.length},null,2));
console.log(JSON.stringify({version,signatures:fixtures.length}));
