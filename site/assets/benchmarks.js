import {renderBenchmarkPanel} from "./benchmark-render.js";

const board=document.querySelector("[data-benchmark-board]");
if(board){
  const data=JSON.parse(document.getElementById("benchmark-data").textContent);
  const panel=board.querySelector("[data-benchmark-panel]");
  const storage=board.querySelector("[data-storage]"),bytes=board.querySelector("[data-bytes]");
  const buttons=[...board.querySelectorAll("[data-view]")];
  let view="agents";
  function update(){
    buttons.forEach(button=>button.setAttribute("aria-pressed",String(button.dataset.view===view)));
    const sizes=view==="agents"?[4096]:data.settings.sizes;
    const selected=sizes.includes(Number(bytes.value))?Number(bytes.value):sizes[0];
    bytes.innerHTML=sizes.map(size=>`<option value="${size}">${size<1048576?size/1024+" KiB":size/1048576+" MiB"}</option>`).join("");
    bytes.value=String(selected);bytes.disabled=sizes.length===1;
    panel.innerHTML=renderBenchmarkPanel(data,{view,storage:storage.value,bytes:selected});
  }
  buttons.forEach(button=>button.addEventListener("click",()=>{view=button.dataset.view;update();}));
  [storage,bytes].forEach(control=>control.addEventListener("change",update));
  update();
}
