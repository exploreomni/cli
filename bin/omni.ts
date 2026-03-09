#!/usr/bin/env tsx

import { cli, command } from 'cleye'
import {
  runConfigInit,
  runConfigShow,
  runConfigUse,
  runModelList,
  runModelValidate,
  runQueryRun,
  runScheduleList,
  runScheduleGet,
  runScheduleTrigger,
  runSchedulePause,
  runScheduleResume,
  runScheduleDelete,
  runScheduleRecipients,
  runUserList,
} from '../src/commands/index.js'
import { resolveOutputMode, outputFlags } from '../src/output/index.js'
import { runTui } from '../src/tui/index.js'

cli(
  {
    name: 'omni-cli',
    version: '0.0.1',
    help: {
      description: 'Omni CLI - AI-powered query assistant and admin tooling',
    },
    commands: [
      command(
        {
          name: 'tui',
          help: {
            description: 'Launch interactive terminal UI',
          },
        },
        () => runTui()
      ),
      command(
        {
          name: 'config:init',
          alias: 'ci',
          help: {
            description: 'Initialize a new profile',
          },
        },
        () => runConfigInit()
      ),
      command(
        {
          name: 'config:show',
          alias: 'cs',
          flags: {
            ...outputFlags,
          },
          help: {
            description: 'Show current configuration',
          },
        },
        (argv) =>
          runConfigShow({
            outputMode: resolveOutputMode({
              format: argv.flags.format,
              noTui: argv.flags['no-tui'],
              noColor: argv.flags['no-color'],
            }),
          })
      ),
      command(
        {
          name: 'config:use',
          alias: 'cu',
          parameters: ['<profile>'],
          flags: {
            ...outputFlags,
          },
          help: {
            description: 'Switch to a different profile',
          },
        },
        (argv) =>
          runConfigUse(argv._.profile, {
            outputMode: resolveOutputMode({
              format: argv.flags.format,
              noTui: argv.flags['no-tui'],
              noColor: argv.flags['no-color'],
            }),
          })
      ),
      command(
        {
          name: 'config',
          flags: {
            ...outputFlags,
          },
          help: {
            description: 'Show current configuration (alias for config:show)',
          },
        },
        (argv) =>
          runConfigShow({
            outputMode: resolveOutputMode({
              format: argv.flags.format,
              noTui: argv.flags['no-tui'],
              noColor: argv.flags['no-color'],
            }),
          })
      ),
      command(
        {
          name: 'model:list',
          alias: 'ml',
          flags: {
            kind: {
              type: String,
              description: 'Filter by model kind (schema, shared, branch)',
            },
            profile: {
              type: String,
              alias: 'p',
              description: 'Profile to use',
            },
            ...outputFlags,
          },
          help: {
            description: 'List models in the organization',
          },
        },
        (argv) =>
          runModelList({
            modelKind: argv.flags.kind,
            profile: argv.flags.profile,
            outputMode: resolveOutputMode({
              format: argv.flags.format,
              noTui: argv.flags['no-tui'],
              noColor: argv.flags['no-color'],
            }),
          })
      ),
      command(
        {
          name: 'model:validate',
          alias: 'mv',
          parameters: ['<modelId>'],
          flags: {
            branch: {
              type: String,
              alias: 'b',
              description: 'Branch ID to validate',
            },
            profile: {
              type: String,
              alias: 'p',
              description: 'Profile to use',
            },
            ...outputFlags,
          },
          help: {
            description: 'Validate a model',
          },
        },
        (argv) =>
          runModelValidate({
            modelId: argv._.modelId,
            branchId: argv.flags.branch,
            profile: argv.flags.profile,
            outputMode: resolveOutputMode({
              format: argv.flags.format,
              noTui: argv.flags['no-tui'],
              noColor: argv.flags['no-color'],
            }),
          })
      ),
      command(
        {
          name: 'query',
          alias: 'q',
          parameters: ['<prompt>'],
          flags: {
            model: {
              type: String,
              alias: 'm',
              description: 'Model ID to query (required)',
            },
            topic: {
              type: String,
              alias: 't',
              description: 'Topic name to use',
            },
            format: {
              type: String,
              alias: 'f',
              description: 'Output format: table, json, or csv',
              default: 'table',
            },
            profile: {
              type: String,
              alias: 'p',
              description: 'Profile to use',
            },
            'no-tui': {
              type: Boolean,
              description: 'Disable interactive TUI output',
            },
            'no-color': {
              type: Boolean,
              description: 'Disable colored output',
            },
          },
          help: {
            description: 'Run an AI-generated query',
          },
        },
        (argv) => {
          if (!argv.flags.model) {
            console.error('Error: --model (-m) is required')
            process.exit(1)
          }
          runQueryRun({
            prompt: argv._.prompt,
            modelId: argv.flags.model,
            topic: argv.flags.topic,
            profile: argv.flags.profile,
            outputMode: resolveOutputMode({
              format: argv.flags.format,
              noTui: argv.flags['no-tui'],
              noColor: argv.flags['no-color'],
            }),
          })
        }
      ),

      // Schedule commands
      command(
        {
          name: 'schedule:list',
          alias: 'sl',
          flags: {
            status: {
              type: String,
              description:
                'Filter by status: success, error, canceled, none',
            },
            destination: {
              type: String,
              description:
                'Filter by destination: email, slack, webhook, sftp',
            },
            search: {
              type: String,
              description: 'Search by name, dashboard, or owner',
            },
            sort: {
              type: String,
              description:
                'Sort field: scheduleName, dashboardName, ownerName, lastRun, lastRunStatus',
            },
            'sort-direction': {
              type: String,
              description: 'Sort direction: asc, desc',
            },
            'page-size': {
              type: Number,
              description: 'Number of results per page',
            },
            page: {
              type: Number,
              description: 'Page number',
            },
            profile: {
              type: String,
              alias: 'p',
              description: 'Profile to use',
            },
            ...outputFlags,
          },
          help: {
            description: 'List schedules',
          },
        },
        (argv) =>
          runScheduleList({
            status: argv.flags.status,
            destination: argv.flags.destination,
            search: argv.flags.search,
            sort: argv.flags.sort,
            sortDirection: argv.flags['sort-direction'],
            pageSize: argv.flags['page-size'],
            page: argv.flags.page,
            profile: argv.flags.profile,
            outputMode: resolveOutputMode({
              format: argv.flags.format,
              noTui: argv.flags['no-tui'],
              noColor: argv.flags['no-color'],
            }),
          })
      ),
      command(
        {
          name: 'schedule:get',
          alias: 'sg',
          parameters: ['<scheduleId>'],
          flags: {
            profile: {
              type: String,
              alias: 'p',
              description: 'Profile to use',
            },
            ...outputFlags,
          },
          help: {
            description: 'Get schedule details',
          },
        },
        (argv) =>
          runScheduleGet({
            scheduleId: argv._.scheduleId,
            profile: argv.flags.profile,
            outputMode: resolveOutputMode({
              format: argv.flags.format,
              noTui: argv.flags['no-tui'],
              noColor: argv.flags['no-color'],
            }),
          })
      ),
      command(
        {
          name: 'schedule:trigger',
          alias: 'st',
          parameters: ['<scheduleId>'],
          flags: {
            profile: {
              type: String,
              alias: 'p',
              description: 'Profile to use',
            },
            ...outputFlags,
          },
          help: {
            description: 'Trigger a schedule immediately',
          },
        },
        (argv) =>
          runScheduleTrigger({
            scheduleId: argv._.scheduleId,
            profile: argv.flags.profile,
            outputMode: resolveOutputMode({
              format: argv.flags.format,
              noTui: argv.flags['no-tui'],
              noColor: argv.flags['no-color'],
            }),
          })
      ),
      command(
        {
          name: 'schedule:pause',
          alias: 'sp',
          parameters: ['<scheduleId>'],
          flags: {
            profile: {
              type: String,
              alias: 'p',
              description: 'Profile to use',
            },
            ...outputFlags,
          },
          help: {
            description: 'Pause a schedule',
          },
        },
        (argv) =>
          runSchedulePause({
            scheduleId: argv._.scheduleId,
            profile: argv.flags.profile,
            outputMode: resolveOutputMode({
              format: argv.flags.format,
              noTui: argv.flags['no-tui'],
              noColor: argv.flags['no-color'],
            }),
          })
      ),
      command(
        {
          name: 'schedule:resume',
          alias: 'sr',
          parameters: ['<scheduleId>'],
          flags: {
            profile: {
              type: String,
              alias: 'p',
              description: 'Profile to use',
            },
            ...outputFlags,
          },
          help: {
            description: 'Resume a paused schedule',
          },
        },
        (argv) =>
          runScheduleResume({
            scheduleId: argv._.scheduleId,
            profile: argv.flags.profile,
            outputMode: resolveOutputMode({
              format: argv.flags.format,
              noTui: argv.flags['no-tui'],
              noColor: argv.flags['no-color'],
            }),
          })
      ),
      command(
        {
          name: 'schedule:delete',
          alias: 'sd',
          parameters: ['<scheduleId>'],
          flags: {
            force: {
              type: Boolean,
              description: 'Skip confirmation prompt',
            },
            profile: {
              type: String,
              alias: 'p',
              description: 'Profile to use',
            },
            ...outputFlags,
          },
          help: {
            description: 'Delete a schedule',
          },
        },
        (argv) =>
          runScheduleDelete({
            scheduleId: argv._.scheduleId,
            force: argv.flags.force,
            profile: argv.flags.profile,
            outputMode: resolveOutputMode({
              format: argv.flags.format,
              noTui: argv.flags['no-tui'],
              noColor: argv.flags['no-color'],
            }),
          })
      ),
      // User commands
      command(
        {
          name: 'user:list',
          alias: 'ul',
          flags: {
            search: {
              type: String,
              description: 'Search by name or email',
            },
            count: {
              type: Number,
              description: 'Number of results to return',
            },
            profile: {
              type: String,
              alias: 'p',
              description: 'Profile to use',
            },
            ...outputFlags,
          },
          help: {
            description: 'List users in the organization',
          },
        },
        (argv) =>
          runUserList({
            search: argv.flags.search,
            count: argv.flags.count,
            profile: argv.flags.profile,
            outputMode: resolveOutputMode({
              format: argv.flags.format,
              noTui: argv.flags['no-tui'],
              noColor: argv.flags['no-color'],
            }),
          })
      ),

      command(
        {
          name: 'schedule:recipients',
          parameters: ['<scheduleId>'],
          flags: {
            profile: {
              type: String,
              alias: 'p',
              description: 'Profile to use',
            },
            ...outputFlags,
          },
          help: {
            description: 'List recipients for a schedule',
          },
        },
        (argv) =>
          runScheduleRecipients({
            scheduleId: argv._.scheduleId,
            profile: argv.flags.profile,
            outputMode: resolveOutputMode({
              format: argv.flags.format,
              noTui: argv.flags['no-tui'],
              noColor: argv.flags['no-color'],
            }),
          })
      ),
    ],
  },
  () => {
    console.log(`
Omni CLI - AI-powered query assistant and admin tooling

Usage:
  omni-cli <command> [options]

Commands:
  tui                 Launch interactive terminal UI
  config              Show current configuration
  config:init (ci)    Initialize a new profile
  config:show (cs)    Show current configuration
  config:use  (cu)    Switch to a different profile
  model:list   (ml)   List models
  model:validate (mv) Validate a model
  query        (q)    Run an AI-generated query
  schedule:list (sl)  List schedules
  schedule:get  (sg)  Get schedule details
  schedule:trigger (st) Trigger a schedule
  schedule:pause (sp) Pause a schedule
  schedule:resume (sr) Resume a schedule
  schedule:delete (sd) Delete a schedule
  schedule:recipients List schedule recipients
  user:list    (ul)   List users

Global Flags:
  --format, -F        Output format: json, csv, or table
  --no-tui            Disable interactive TUI output
  --no-color          Disable colored output

Run 'omni-cli <command> --help' for more information on a command.
`)
  }
)
