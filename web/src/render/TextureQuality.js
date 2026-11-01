import * as THREE from 'three';

// Older world packs stored screens as DataTextures without mipmaps. Repair
// sampling after loading, preserving their authored UV transform and colour space.
export function configureTexture(texture, renderer) {
 if(!texture?.isTexture||texture.isCubeTexture||texture.isRenderTargetTexture)return texture;
 const image=texture.image;
 if(!image?.width||!image?.height||texture.type!==THREE.UnsignedByteType)return texture;
 const anisotropy=renderer?.isSoftware?1:Math.min(4,renderer?.capabilities?.getMaxAnisotropy?.()||1);
 if(texture.generateMipmaps&&texture.minFilter===THREE.LinearMipmapLinearFilter&&texture.magFilter===THREE.LinearFilter&&texture.anisotropy===anisotropy)return texture;
 texture.generateMipmaps=true;
 texture.minFilter=THREE.LinearMipmapLinearFilter;
 texture.magFilter=THREE.LinearFilter;
 texture.anisotropy=anisotropy;
 texture.needsUpdate=true;
 return texture;
}
