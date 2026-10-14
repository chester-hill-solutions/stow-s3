import assert from "node:assert/strict";
import {createServer} from "node:http";
import {readFile,mkdir,writeFile} from "node:fs/promises";
import {resolve,extname} from "node:path";
import {chromium} from "../packages/stow-s3/node_modules/playwright/index.mjs";
import {renderBenchmarkSection} from "../site/assets/benchmark-render.js";

// Browser verification runs only inside the Railway verification build.
assert.equal(process.cwd(),"/src");assert.equal(process.env.BENCH_CLOUD_VERIFY,"1");
const root=resolve("site/.build"),out="/out/verification/preview";
await mkdir(out,{recursive:true});
const data=JSON.parse(await readFile("site/benchmark-data.json","utf8"));
const styles=(await Promise.all(["theme.css","stow.css","benchmarks.css"].map(name=>readFile(`site/assets/${name}`,"utf8")))).join("\n");
const renderer=(await readFile("site/assets/benchmark-render.js","utf8")).replace(/^export /gm,"");
const controls=(await readFile("site/assets/benchmarks.js","utf8")).replace(/^import[^\n]*\n/,"");
const moduleSource=(renderer+"\n"+controls).replace(/<\/script/gi,"<\\/script");
const sectionHtml=renderBenchmarkSection(data).replace('href="/assets/benchmark-data.json"',`href="data:application/json;charset=utf-8,${encodeURIComponent(JSON.stringify(data))}"`);
await writeFile(`${out}/preview.html`,`<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><link rel="icon" href="data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg'/%3E"><title>Stow benchmark · private preview</title><style>${styles}</style></head><body><main>${sectionHtml}</main><script type="module">${moduleSource}</script></body></html>`);
const types={".html":"text/html",".js":"text/javascript",".css":"text/css",".json":"application/json",".svg":"image/svg+xml",".png":"image/png"};
const server=createServer(async(req,res)=>{
  try{
    const url=new URL(req.url,"http://localhost");
    const privatePreview=url.pathname==="/private-preview.html";
    const file=privatePreview?`${out}/preview.html`:resolve(root,"."+decodeURIComponent(url.pathname),url.pathname.endsWith("/")?"index.html":"");
    if(!privatePreview&&!file.startsWith(root+"/"))throw new Error("outside root");
    const body=await readFile(file);res.writeHead(200,{"content-type":types[extname(file)]??"application/octet-stream"});res.end(body);
  }catch{res.writeHead(404);res.end("missing");}
});
await new Promise(done=>server.listen(0,"127.0.0.1",done));
const browser=await chromium.launch({args:["--no-sandbox","--disable-dev-shm-usage"]});
const errors=[];
let combinations=0;
try{
  const page=await browser.newPage({viewport:{width:1440,height:1100},reducedMotion:"reduce"});
  page.on("pageerror",error=>errors.push(error.message));
  page.on("console",message=>{if(message.type()==="error")errors.push(message.text());});
  await page.goto(`http://127.0.0.1:${server.address().port}/`,{waitUntil:"networkidle"});
  const dataset=await page.locator("#benchmark-data").textContent();
  assert.deepEqual(JSON.parse(dataset),JSON.parse(await readFile("site/benchmark-data.json","utf8")));
  assert.equal(data.comparisonScope,"S3-shaped agent storage");
  assert.equal(await page.locator("[data-lane]").count(),0);
  for(const key of ["embedded","goAgent","nodeAgent"])assert.equal(key in data,false);
  assert.ok(Object.values(data.names).every(name=>["Stow","MinIO","S3Proxy","RustFS"].includes(name)));
  const section=page.locator("#performance"),board=page.locator("[data-benchmark-board]");
  const select=(name,value)=>board.locator(`[data-${name}]`).selectOption(value);
  const view=name=>board.locator(`[data-view=${name}]`).click();
  async function check(){
    assert.equal(await board.locator('[data-view][aria-pressed="true"]').count(),1);
    assert.ok(await board.locator(".benchmark-chart").count()>0);
    const expected=await board.locator("[data-storage]").inputValue()==="filesystem"?["MinIO","RustFS","S3Proxy","Stow"]:["S3Proxy","Stow"];
    for(const figure of await board.locator(".benchmark-chart").all()){
      assert.deepEqual((await figure.locator(".benchmark-name").allTextContents()).sort(),expected);
    }
    assert.equal(/SQLite|Afero|buffers/i.test(await board.textContent()),false);
    const bad=await board.locator(".benchmark-value").allTextContents();
    assert.ok(bad.every(value=>!/(NaN|undefined|Infinity)/.test(value)));
    assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),"horizontal overflow");
    const valid=await board.locator(".benchmark-bar").evaluateAll(nodes=>nodes.every(node=>{const width=parseFloat(node.style.width);return Number.isFinite(width)&&width>0&&width<=100;}));
    assert.equal(valid,true);combinations++;
  }
  await check();await section.screenshot({path:`${out}/desktop-startup.png`});
  for(const storage of ["filesystem","memory"]){
    await select("storage",storage);
    for(const workload of ["agents","transfer","quarantine","memory"]){
      await view(workload);
      const sizes=await board.locator("[data-bytes] option").evaluateAll(nodes=>nodes.map(node=>node.value));
      for(const bytes of sizes){if(sizes.length>1)await select("bytes",bytes);await check();}
    }
  }
  await select("storage","filesystem");
  for(const workload of ["transfer","quarantine","memory"]){await view(workload);await section.screenshot({path:`${out}/desktop-${workload}.png`});}
  await page.setViewportSize({width:390,height:1000});
  await select("storage","filesystem");await view("agents");await check();
  await section.screenshot({path:`${out}/mobile-startup.png`});
  await view("transfer");await check();await section.screenshot({path:`${out}/mobile-transfer.png`});
  await page.goto(`http://127.0.0.1:${server.address().port}/private-preview.html`,{waitUntil:"networkidle"});
  await check();await select("storage","memory");await view("memory");await check();
  assert.deepEqual(errors,[]);
  await writeFile(`${out}/receipt.json`,JSON.stringify({complete:true,combinations,browser:await browser.version(),screenshots:6,comparisonScope:data.comparisonScope,errors},null,2)+"\n");
  console.log(`Railway browser verification passed: ${combinations} chart combinations, desktop + mobile, 6 previews`);
}finally{await browser.close();await new Promise(done=>server.close(done));}
