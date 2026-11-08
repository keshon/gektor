"""Build editable composer proposals in the existing FigCraft library.

Usage: python docs/design/tui/update-composer.py --output tmp/composer-review.figcraft
Requires the installed FigCraft CLI. Regenerates only '06 Composer review';
manual changes on that page are replaced when explicitly running this script.
"""
import argparse
import json
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

ROOT = Path(__file__).resolve().parents[3]
SOURCE = ROOT / 'docs/design/tui/gektor-ui.figcraft'
PAGE = '06 Composer review'
COLLECTION = 'VariableCollectionId:1'
ROLE = {'canvas':1, 'surface':2, 'text':3, 'secondary':4, 'border':5, 'accent':6,
        'selection':7, 'selection-text':8, 'success':9, 'warning':10, 'danger':11, 'disabled':12}
COLOR = {'canvas':'#11161C', 'surface':'#263744', 'text':'#D5DEE7', 'secondary':'#9DABB7',
         'border':'#8096A8', 'accent':'#79C8D0', 'selection':'#3B5268',
         'selection-text':'#F1F4F8', 'success':'#82C995', 'warning':'#E6C374',
         'danger':'#E58B8B', 'disabled':'#667886'}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--cli', default=shutil.which('figcraft-cli') or
                        ('C:/Program Files/ArtCraft/FigCraft/figcraft-cli.exe' if sys.platform == 'win32' else None))
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    if not args.cli:
        parser.error("Set --cli to the installed FigCraft CLI")
    args.output.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='gektor-composer-') as scratch:
        scratch = Path(scratch)
        sequence = 0

        def run(commands, source, output=None):
            nonlocal sequence
            sequence += 1
            script = scratch / f'{sequence}.json'
            script.write_text(json.dumps(commands, ensure_ascii=False, indent=2), encoding='utf-8')
            command = [args.cli, 'run', str(script), '--in', str(source), '--no-system-fonts']
            if output is not None:
                command += ['--out', str(output)]
            result = subprocess.run(command, capture_output=True, text=True, encoding='utf-8', check=True)
            records = [json.loads(line) for line in result.stdout.splitlines() if line.strip()]
            if len(records) != len(commands) or any(not row.get('ok') or row.get('rolledBack') for row in records):
                raise RuntimeError(result.stdout + result.stderr)
            return [row['result'] for row in records]

        pages = run([{'command':'page.list','params':{}}], SOURCE)[0]
        # page.list returns a page list wrapped in 'pages'.
        pages = pages.get('pages', []) if isinstance(pages, dict) else pages
        commands = [{'command':'page.delete','params':{'id':page['id']}}
                    for page in pages if page['name'] == PAGE]
        commands += [{'command':'page.add','params':{'name':PAGE}}]
        base = scratch / 'page.figcraft'
        page = run(commands, SOURCE, base)[-1]['id']

        boards = [
            ('Composer / Queued / Light',800,480,0,0,'Light','queued'),
            ('Composer / Queued / Dark',800,480,900,0,'Dark','queued'),
            ('Composer / Working / Light',800,480,0,600,'Light','working'),
            ('Composer / Received / Light',800,480,900,600,'Light','received'),
            ('Composer / Queued narrow / Light',400,400,1800,0,'Light','narrow'),
            ('Composer / Draft narrow / Light',400,400,2300,0,'Light','draft'),
            ('Composer / Queued narrow / Dark',400,400,1800,600,'Dark','narrow'),
            ('Composer comparison / Light',1000,560,0,1200,'Light','comparison'),
            ('Composer review notes',800,440,1100,1200,'Light','notes'),
            ('Message queue / Light',800,480,0,2400,'Light','queue'),
            ('Message queue / Dark',800,480,900,2400,'Dark','queue'),
            ('Message queue / Narrow',400,400,1800,2400,'Light','queue-narrow'),
            ('Message queue / Narrow dark',400,400,2300,2400,'Dark','queue-narrow'),
            ('Queued message / Read',800,480,0,3000,'Light','read'),
            ('Queued message / Edit',800,480,900,3000,'Light','edit'),
            ('Message queue / Removed',800,480,0,3600,'Light','removed'),
            ('Message queue / Received',800,480,900,3600,'Light','queue-received'),
            ('Composer / Two queued',800,480,1800,3000,'Light','two'),
            ('Message queue / Edited',800,480,0,4200,'Light','edited'),
            ('Queued message / Read edited',800,480,900,4200,'Light','read-edited'),
            ('Queued message / Received',800,480,1800,3600,'Light','read-received'),
        ]
        masters = [('Component / Composer queued',760,40),
                   ('Component / Run shortcuts queued',800,20),
                   ('Component / Run shortcuts working',800,20)]
        create = []
        for name,w,h,x,y,theme,state in boards:
            create.append({'command':'frame.create','params':{'parent':page,'name':name,'width':w,'height':h,
                          'x':x,'y':y,'props':{'fill':COLOR['canvas'],'clip':True,
                          'grids':[{'kind':'grid','size':10,'visible':False,'color':{'r':1,'g':0,'b':0,'a':0.1},'count':5,'gutter':20,'align':'stretch','offset':0}]}}})
        for index,(name,w,h) in enumerate(masters):
            create.append({'command':'node.create','params':{'type':'component','parent':page,'name':name,
                          'width':w,'height':h,'x':index*900,'y':1900,
                          'props':{'fill':COLOR['canvas'],'clip':True}}})
        frames = scratch / 'frames.figcraft'
        ids = [item['id'] for item in run(create, base, frames)]
        master = dict(zip((m[0] for m in masters), ids[len(boards):]))
        commands, bindings, links = [], [], []
        board_ids = {board[0]:node for board,node in zip(boards,ids)}

        def link(step, destination):
            links.append((step, destination))

        def keyboard(frame,key,destination):
            links.append((None,destination,frame,key))

        def emit(command, params, role=None, field='fill'):
            index = len(commands)
            commands.append({'command':command,'params':params})
            if role:
                bindings.append((index, role, field))
            return index

        def rect(parent,name,x,y,w,h,role):
            return emit('node.create', {'type':'rectangle','parent':parent,'name':name,'x':x,'y':y,
                 'width':w,'height':h,'props':{'fill':COLOR[role]}}, role)

        def text(parent,name,value,x,y,w,role='text',bold=False,h=20):
            return emit('text.create',{'parent':parent,'name':name,'text':value,'x':x,'y':y,'width':w+2,
                 'props':{'height':h,'fontFamily':'Liberation Mono','fontStyle':'Bold' if bold else 'Regular',
                          'fontSize':16.66,'lineHeight':20,'textAutoResize':'none','fill':COLOR[role]}},role)

        def instance(parent, component, x, y):
            emit('instance.create',{'parent':parent,'component':master[component],'x':x,'y':y})

        def toolbar(parent,width,y,actions,narrow=False,offset=0,targets=None,tracks=None):
            if tracks is None:
                tracks = [130,130,120] if narrow else ([190,190,190,200] if len(actions)==4 else [260,260,260])
            x = offset
            for index,((key,label,enabled),w) in enumerate(zip(actions,tracks)):
                keyw = (len(key)+2)*10
                steps = [rect(parent,f'Hotkey {index+1} / Key surface',x,y,keyw,20,'surface'),
                         rect(parent,f'Hotkey {index+1} / Action surface',x+keyw,y,w-keyw,20,'selection' if enabled else 'surface'),
                         text(parent,f'Hotkey {index+1} / Key',' '+key+' ',x,y,keyw,'text' if enabled else 'disabled',True),
                         text(parent,f'Hotkey {index+1} / Action',label,x+keyw,y,w-keyw,'selection-text' if enabled else 'disabled')]
                if targets and targets[index] and enabled:
                    for step in steps:link(step,targets[index])
                x += w+10

        queued_actions = [('ESC','Stop',True),('ENTER','Send',True),('ALT+Q','Queue',True),('F1','Help',True)]
        working_actions = [('ESC','Stop',True),('ENTER','Send',True),('CTRL+O','Newline',True),('F1','Help',True)]
        component = master['Component / Composer queued']
        text(component,'Queued message preview',' · Latest: Check the light theme too',80,0,680,'secondary')
        text(component,'Input prompt','> ',0,20,20,'accent')
        text(component,'Empty draft placeholder','Send another message…',20,20,720,'secondary')
        rect(component,'Input caret',20,38,10,1,'accent')
        toolbar(master['Component / Run shortcuts queued'],800,0,queued_actions)
        toolbar(master['Component / Run shortcuts working'],800,0,working_actions)

        for node in master.values():
            bindings.append((None,'canvas','fill',node))

        def queue_screen(frame,w,h,theme,state):
            narrow = w==400
            destination = 'Message queue / Narrow' if narrow else 'Message queue / '+theme
            if state in ('removed','edited','queue-received'):destination = {'removed':'Message queue / Removed','edited':'Message queue / Edited','queue-received':'Message queue / Received'}[state]
            rect(frame,'Menu surface',0,0,w,20,'surface')
            text(frame,'Application menus','GEKTOR  Session  Run  More…' if narrow else 'GEKTOR   Session   Run   View   Settings   Help',10,0,w-20)
            text(frame,'Preserved composer draft','> Add a short summary too.',20,h-60,w-40,'secondary')
            if narrow:
                x,y,mw,mh=10,20,380,340
            else:
                x,y,mw,mh=40,40,720,380
            rect(frame,'Queue dialog surface',x,y,mw,mh,'surface')
            rect(frame,'Queue footer divider',x,y+(290 if narrow else 330),mw,1,'border')
            for bx,by,bw,bh in [(x,y,mw,1),(x,y,1,mh),(x+mw,y,1,mh),(x,y+mh,mw,1),(x,y+60,mw,1)]:
                rect(frame,'Queue dialog border',bx,by,bw,bh,'border')
            title='Message queue'
            if state in ('read','read-edited'):title='Queued message 1 of 3'
            if state=='read-received':title='Received message 1 of 3'
            if state=='edit':title='Edit queued message'
            text(frame,'Queue dialog title',title,x+20,y+20,mw-70,'accent',True)
            close_target = 'Composer / Two queued' if state=='removed' else (('Composer / Queued narrow / Dark' if theme=='Dark' else 'Composer / Draft narrow / Light') if narrow else 'Composer / Queued / '+theme)
            link(text(frame,'Close queue','×',x+mw-30,y+20,20),close_target)
            if state not in ('read','read-edited','read-received','edit'):keyboard(frame,'Escape',close_target)
            original='Compare these screenshots. Tell me which labels are unclear or hard to read.'
            edited='Compare these screenshots and flag unclear labels.'
            if state in ('read','read-edited','read-received','edit'):
                text(frame,'Message state','Received · read-only · 1 image' if state=='read-received' else 'Queued · 1 image',x+20,y+80,mw-40,'secondary')
                if state=='edit':
                    text(frame,'Editor label','Message',x+20,y+120,mw-40,'text',True)
                    rect(frame,'Message editor',x+20,y+150,mw-40,100,'canvas')
                    text(frame,'Editable message',edited,x+30,y+160,mw-60,h=60)
                    text(frame,'Attachment retained','1 image kept: comparison.png',x+20,y+270,mw-40,'secondary')
                    text(frame,'Draft preservation note','Your composer draft stays unchanged.',x+20,y+300,mw-40,'secondary')
                    toolbar(frame,mw-40,y+340,[('CTRL+S','Save',True),('ESC','Cancel',True)],offset=x+20,targets=['Message queue / Edited','Message queue / Light'],tracks=[330,340])
                    keyboard(frame,'Ctrl+S','Message queue / Edited')
                    keyboard(frame,'Escape','Message queue / Light')
                else:
                    text(frame,'Full message',edited if state=='read-edited' else original,x+20,y+130,mw-40,h=80)
                    text(frame,'Attachment label','Attachment',x+20,y+240,mw-40,'text',True)
                    text(frame,'Attachment filename','comparison.png',x+20,y+270,mw-40,'secondary')
                    back='Message queue / Received' if state=='read-received' else ('Message queue / Edited' if state=='read-edited' else 'Message queue / Light')
                    toolbar(frame,mw-40,y+340,[('CTRL+E','Edit',state!='read-received'),('DEL','Remove',state!='read-received'),('ESC','Back',True)],offset=x+20,targets=['Queued message / Edit','Message queue / Removed',back],tracks=[220,220,220])
                    keyboard(frame,'Escape',back)
                return
            rows=[('Compare these screenshots','Queued · 1 image'),('Check the inspector spacing','Queued'),('Check the light theme too','Queued')]
            if state=='removed':rows=rows[1:]
            if state=='edited':rows[0]=('Compare screenshots and flag labels','Queued · 1 image')
            if state=='queue-received':rows[0]=(rows[0][0],'Received · read-only · 1 image')
            count='2 queued · 1 received' if state=='queue-received' else f'{len(rows)} queued'
            text(frame,'Queue count and order',count+' · oldest first',x+20,y+80,mw-40,'secondary')
            for index,(message,metadata) in enumerate(rows):
                ry=y+120+index*60
                if index==0:rect(frame,'Selected message',x+20,ry,mw-40,40,'selection')
                message = message if not narrow else ['Compare these screenshots','Check inspector spacing','Check the light theme too'][index]
                title=text(frame,f'Message {index+1} title',f'{index+1}  '+message,x+30,ry,mw-60,'selection-text' if index==0 else 'text',index==0)
                text(frame,f'Message {index+1} status',metadata,x+60,ry+20,mw-90,'selection-text' if index==0 else 'secondary')
                if index==0:link(title,'Queued message / Received' if state=='queue-received' else ('Queued message / Read edited' if state=='edited' else 'Queued message / Read'))
            can_edit = state!='queue-received'
            read_target = 'Queued message / Received' if state=='queue-received' else ('Queued message / Read edited' if state=='edited' else 'Queued message / Read')
            if narrow:
                toolbar(frame,mw-40,y+300,[('ENTER','Read',True),('CTRL+E','Edit',True)],offset=x+20,targets=[read_target,'Queued message / Edit'],tracks=[160,170])
                toolbar(frame,mw-40,y+320,[('DEL','Remove',True),('ESC','Close',True)],offset=x+20,targets=['Message queue / Removed',close_target],tracks=[160,170])
            else:
                toolbar(frame,mw-40,y+340,[('ENTER','Read',True),('CTRL+E','Edit',can_edit),('DEL','Remove',can_edit),('ESC','Close',True)],offset=x+20,
                        targets=[read_target,'Queued message / Edit','Message queue / Removed',close_target],tracks=[160,160,160,170])
            keyboard(frame,'Enter',read_target)
            if can_edit:
                keyboard(frame,'Ctrl+E','Queued message / Edit')
                keyboard(frame,'Delete','Message queue / Removed')

        for spec,frame in zip(boards,ids):
            name,w,h,x,y,theme,state = spec
            bindings.append((None,'canvas','fill',frame))
            if state=='notes':
                text(frame,'Review title','Composer review',20,20,760,'accent',True)
                notes=[
                    'The summary counts all queued messages and previews the latest.',
                    'Click the count or press ALT+Q to open the queue.',
                    'The list is oldest first. Select any queued message.',
                    'Shortcuts appear once, in the bottom action bar.',
                    'Received means the message cannot be recalled.',
                    'Edit has its own editor; the composer draft stays intact.',
                    'Remove affects only the selected queued message.',
                    'At 40 columns, Stop, Send and Queue stay visible.',
                    'Selections follow message IDs when the queue changes.',
                    'F1 Help and CTRL+O newline still work in every state.',
                    'Examples are static. Application code is unchanged.',
                ]
                for i,line in enumerate(notes):text(frame,f'Review note {i+1}',line,20,80+i*30,760,'secondary')
                continue
            if state=='comparison':
                text(frame,'Comparison title','Composer review / Light',20,20,960,'accent',True)
                text(frame,'Before heading','Before: repeated controls compete with the draft',100,60,800,'text',True)
                for i,line in enumerate(['ESC stop  ENTER queue  CTRL+O newline','Pending 3 · latest: Check the light theme too','CTRL+E edit latest  CTRL+X cancel latest','> _']):
                    text(frame,f'Before row {i+1}',line,120,100+i*20,760,'secondary' if i<3 else 'text')
                rect(frame,'Before divider',100,190,800,1,'border')
                toolbar(frame,800,200,[('F1','Help',True),('CTRL+P','Search',True),('CTRL+N','New',False)],offset=100)
                text(frame,'After heading','Proposed: queue, draft, then actions',100,280,800,'text',True)
                instance(frame,'Component / Composer queued',120,340)
                link(text(frame,'Open queue','Queued 3',120,340,80,'accent',True),'Message queue / Light')
                rect(frame,'After divider',100,390,800,1,'border')
                # Native component stays 80 cells wide; its transform is relative to the board.
                toolbar(frame,800,400,queued_actions,offset=100,targets=[None,None,'Message queue / Light',None])
                text(frame,'Comparison note','Two text rows recovered. The gaps between hotkeys remain.',100,480,800,'secondary')
                continue
            if state in ('queue','queue-narrow','removed','queue-received','edited','read','read-edited','read-received','edit'):
                queue_screen(frame,w,h,theme,state)
                continue
            narrow = w==400
            rect(frame,'Menu surface',0,0,w,20,'surface')
            text(frame,'Application menus','GEKTOR  Session  Run  More…' if narrow else 'GEKTOR   Session   Run   View   Settings   Help',10,0,w-20)
            for bx,by,bw,bh in [(5,30,w-10,1),(5,30,1,h-60),(w-5,30,1,h-60),(5,h-30,w-10,1),(5,70,w-10,1)]:
                rect(frame,'Pane border',bx,by,bw,bh,'border')
            text(frame,'Run state','ACT  WORKING  Ornith 1.5',20,40,w-40,'secondary')
            if not narrow:text(frame,'Step budget','STEP 8/25',650,40,130,'secondary')
            text(frame,'Agent label','GEKTOR',20,100,w-40,'accent',True)
            lines=['I am checking the image references.','The current tool call is finishing.']
            for i,line in enumerate(lines):text(frame,f'Conversation line {i+1}',line,20,140+i*20,w-40)
            text(frame,'Active tool','Reading image metadata…',20,220,w-40,'secondary')
            footer_y = h-100 if state!='working' else h-80
            rect(frame,'Composer divider',5,footer_y+10,w-10,1,'border')
            if state in ('queued','two'):
                destination = 'Message queue / Removed' if state=='two' else 'Message queue / '+theme
                if state=='two':
                    link(text(frame,'Open queue','Queued 2',20,h-80,80,'accent',True),destination)
                    text(frame,'Latest queued message',' · Latest: Check the light theme too',100,h-80,w-120,'secondary')
                    text(frame,'Draft','> Add a short summary too.',20,h-60,w-40)
                else:
                    instance(frame,'Component / Composer queued',20,h-80)
                    link(text(frame,'Open queue','Queued 3',20,h-80,80,'accent',True),destination)
                toolbar(frame,w,h-20,queued_actions,targets=[None,None,destination,None])
                keyboard(frame,'Alt+Q',destination)
            elif state in ('narrow','draft'):
                queue_dest='Message queue / Narrow dark' if theme=='Dark' else 'Message queue / Narrow'
                link(text(frame,'Open queue','Queued 3',20,h-80,80,'accent',True),queue_dest)
                text(frame,'Queued message preview',' · Latest: Check theme…',100,h-80,w-120,'secondary')
                keyboard(frame,'Alt+Q',queue_dest)
                text(frame,'Input prompt','> ',20,h-60,20,'accent')
                text(frame,'Draft' if state=='draft' else 'Empty draft placeholder',
                     'Add a short summary too.' if state=='draft' else 'Send another message…',40,h-60,w-60,
                     'text' if state=='draft' else 'secondary')
                rect(frame,'Input caret',280 if state=='draft' else 40,h-42,10,1,'accent')
                toolbar(frame,w,h-20,[('ESC','Stop',True),('ENTER','Send',True),('ALT+Q','Queue',True)],True,targets=[None,None,queue_dest])
            else:
                if state=='received':
                    link(text(frame,'Open queue','Queued 2',20,h-80,80,'accent',True),'Message queue / Received')
                    text(frame,'Received state',' · Received 1 · Latest: Check the light theme too',100,h-80,w-120,'secondary')
                text(frame,'Input prompt','> ',20,h-60,20,'accent')
                text(frame,'Empty draft placeholder','Send another message…',40,h-60,w-60,'secondary')
                rect(frame,'Input caret',40,h-42,10,1,'accent')
                if state=='received':
                    toolbar(frame,w,h-20,queued_actions,targets=[None,None,'Message queue / Received',None])
                    keyboard(frame,'Alt+Q','Message queue / Received')
                else:instance(frame,'Component / Run shortcuts working',0,h-20)

        content = scratch / 'content.figcraft'
        results = run(commands,frames,content)
        final = []
        for binding in bindings:
            if binding[0] is None:
                _,role,field,node = binding
            else:
                step,role,field=binding
                node=results[step]['id']
            final.append({'command':'variable.bind','params':{'ids':[node],'field':field,'variable':f'VariableID:{ROLE[role]}'}})
        for board,node in zip(boards,ids):
            final.append({'command':'variable.setMode','params':{'ids':[node],'collection':COLLECTION,'mode':board[5]}})
        board_specs={node:board for board,node in zip(boards,ids)}
        for entry in links:
            if entry[0] is None:
                _,destination,node,key=entry
                params={'id':node,'trigger':{'type':'keyPress','key':key},'action':{'type':'navigate','dest':board_ids[destination]}}
            else:
                step,destination=entry
                params={'id':results[step]['id'],'dest':board_ids[destination]}
            source=commands[entry[0]]['params']['parent'] if entry[0] is not None else entry[2]
            source_spec=board_specs.get(source)
            target_spec=next(board for board in boards if board[0]==destination)
            if source_spec and (source_spec[1]!=target_spec[1] or source_spec[5]!=target_spec[5]):continue
            if source_spec and source_spec[6]=='queue-narrow' and target_spec[6] in ('narrow','draft'):
                params.pop('dest',None)
                params['action']={'type':'back'}
            final.append({'command':'proto.interaction.add','params':params})
        run(final,content,args.output)
        print(f'Wrote {args.output}; {len(boards)} boards and {len(masters)} native components')


if __name__=='__main__':
    main()
