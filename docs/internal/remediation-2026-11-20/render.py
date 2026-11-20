from PIL import Image, ImageDraw, ImageFont
from pathlib import Path
import re
# Approximate ANSI fixture previews using Windows fonts, not terminal captures.
root=Path(__file__).resolve().parent
font=ImageFont.truetype(r'C:\Windows\Fonts\consola.ttf',18)
bold=ImageFont.truetype(r'C:\Windows\Fonts\consolab.ttf',18)
italic=ImageFont.truetype(r'C:\Windows\Fonts\consolai.ttf',18)
symbol=ImageFont.truetype(r'C:\Windows\Fonts\seguisym.ttf',18)
rx=re.compile(r'\x1b\[([0-9;:]*)m')
for name in ['chat-conversation-dark-120x40','chat-conversation-light-80x24','chat-diff-dark-80x24','chat-badges-light-80x24','compact-agent-light-40x20']:
 s=(root/(name+'.ansi')).read_text(encoding='utf8')
 lines=s.splitlines(); plain=[rx.sub('',l) for l in lines]
 bg0=(242,244,246) if 'light' in name or 'actions' in name else (17,22,28)
 fg0=(35,45,56) if bg0[0]>100 else (213,222,231)
 im=Image.new('RGB',(max(map(len,plain))*11+24,len(lines)*23+24),bg0);d=ImageDraw.Draw(im)
 fg,bg=fg0,bg0; bo=it=False
 for y,line in enumerate(lines):
  x=0; end=0
  for m in list(rx.finditer(line))+[None]:
   start=m.start() if m else len(line)
   for c in line[end:start]:
    d.rectangle((12+x*11,12+y*23,12+(x+1)*11-1,12+(y+1)*23-1), fill=bg)
    d.text((12+x*11,12+y*23),c,font=symbol if ord(c) in (0x2713,0x2715,0x25cf,0x2022) else bold if bo else italic if it else font,fill=fg);x+=1
   if not m: break
   v=[int(a or 0) for a in m.group(1).split(';')];j=0
   while j<len(v):
    n=v[j]
    if n==0:fg,bg=fg0,bg0;bo=it=False
    elif n==1:bo=True
    elif n==3:it=True
    elif n==22:bo=False
    elif n==23:it=False
    elif n==39:fg=fg0
    elif n==49:bg=bg0
    elif n in (38,48) and j+4<len(v) and v[j+1]==2:
     col=tuple(v[j+2:j+5]);fg=col if n==38 else fg;bg=col if n==48 else bg;j+=4
    j+=1
   end=m.end()
 im.save(root/(name+'.png'))
print('rendered chat previews')
