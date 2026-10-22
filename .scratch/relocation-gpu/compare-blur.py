from PIL import Image
import numpy as np,json
from pathlib import Path
p=Path(__file__).resolve().parent/'evidence'/'blur-preserved'
a=np.asarray(Image.open(p/'shared-before-tiled.png').convert('RGB'),dtype=float)
b=np.asarray(Image.open(p/'shared-after-tiled.png').convert('RGB'),dtype=float)
h,w=a.shape[:2]
mask=np.ones((h,w),bool);mask[:50,:]=False;mask[480:730,850:1710]=False;mask[-110:,:420]=False
d=np.abs(a-b).mean(axis=2)
seams=np.zeros((h,w),bool)
for i in range(1,8): seams[:,int(w*i/8)-8:int(w*i/8)+8]=True
for i in range(1,4): seams[int(h*i/4)-8:int(h*i/4)+8,:]=True
result={'width':w,'height':h,'scope':'static background excluding foreground dialog, probe toolbar, and live task bubble','mean_absolute_RGB_difference':round(d[mask].mean(),4),'p99_absolute_RGB_difference':round(float(np.quantile(d[mask],.99)),4),'tile_boundary_mean_difference':round(d[mask&seams].mean(),4),'tile_boundary_p99_difference':round(float(np.quantile(d[mask&seams],.99)),4)}
print(json.dumps(result))
if result['mean_absolute_RGB_difference']>0.1 or result['tile_boundary_p99_difference']>3:raise SystemExit('Visual mismatch')
