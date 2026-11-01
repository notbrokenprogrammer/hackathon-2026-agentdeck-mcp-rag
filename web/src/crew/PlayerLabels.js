import {CrewLabels} from './CrewLabels.js';

// The authenticated snapshot already contains the public nickname. Keep it as
// text, never HTML, and attach the label to the interpolated avatar position.
export class PlayerLabels extends CrewLabels {
 constructor(container){super(container);if(this.layer)this.layer.className='player-label-layer';}
 sync(players){
  const present=new Set();
  for(const [id,item]of players){
   present.add(id);const element=this.element({id,short:item.name||'Игрок',role:'Игрок'});
   element.className='player-nameplate';element.dataset.playerId=id;
   element.children[0].textContent=item.name||'Игрок';
   Object.assign(element.style,{borderColor:'rgba(144,219,244,.7)',maxWidth:'280px',overflow:'hidden',textOverflow:'ellipsis'});
  }
  for(const [id,element]of this.entries)if(!present.has(id)){element.remove();this.entries.delete(id);}
 }
 updatePlayers(camera,players,occluders,options){
  const characters=[];
  for(const [id,item]of players)characters.push({profile:{id,short:item.name||'Игрок',role:'Игрок'},group:item.mesh,loaded:true,rig:{head:item.nameAnchor}});
  this.update(camera,characters,occluders,options);
 }
}
