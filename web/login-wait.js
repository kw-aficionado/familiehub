/* Shared countdown for login and tablet pairing. Never stores credentials. */
(function(root,factory){var api=factory();if(typeof module==='object'&&module.exports)module.exports=api;else root.FamilyHubLoginWait=api})(typeof window!=='undefined'?window:this,function(){
 'use strict';
 function seconds(value){var n=Number(value);return Number.isFinite(n)&&n>0?Math.min(Math.ceil(n),86400):0}
 function remaining(until,now){return Math.max(0,Math.ceil((until-now)/1000))}
 function install(button,status){
  var until=0,timer=null,busy=false;
  function render(){var n=remaining(until,Date.now());button.disabled=busy||n>0;if(n){status.textContent='Vent '+Math.floor(n/60)+':'+String(n%60).padStart(2,'0')+' f\u00f8r n\u00e6ste fors\u00f8g.'}else{status.textContent='';if(timer!==null){clearInterval(timer);timer=null}}}
  return {blocked:function(){return busy||remaining(until,Date.now())>0},busy:function(value){busy=!!value;render()},start:function(value){var n=seconds(value);if(!n)return;until=Date.now()+n*1000;if(timer===null)timer=setInterval(render,1000);render()},clear:function(){until=0;render()}};
 }
 return {seconds:seconds,remaining:remaining,install:install};
});
