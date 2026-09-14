import { FormattedMessage } from "react-intl";
import { Dialog } from "@/components/ui/dialog";
import { TvDialogContent } from "./tv-dialog";

export default function TvHelp({ close }: { close: () => void }) {
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) close();
      }}
    >
      <TvDialogContent
        className="sm:max-w-xl"
        title={
          <FormattedMessage id="tv.text.tv_guide" defaultMessage="TV guide" />
        }
        description={
          <FormattedMessage
            id="tv.text.scenes_and_markers_in_a_continuous_vertical_feed"
            defaultMessage="Scenes and markers in a continuous vertical feed."
          />
        }
      >
        <div className="flex flex-col gap-4" data-selectable-text>
          <p>
            <FormattedMessage
              id="tv.text.swipe_up_for_the_next_item_and_down_for_the"
              defaultMessage="Swipe up for the next item and down for the previous item. A wheel or trackpad gesture moves one item. Pinch or double-tap the video to zoom; drag to pan while zoomed. Reset zoom before swiping the feed."
            />
          </p>
          <p>
            <FormattedMessage
              id="tv.text.tap_and_hold_playback"
              defaultMessage="Tap the video to play or pause. Hold it for 2× speed, then release to restore playback. Drag the scrubber to seek. Mute and navigation stay available at the bottom."
            />
          </p>
          <table className="w-full text-left">
            <caption className="sr-only">
              <FormattedMessage
                id="tv.text.keyboard_shortcuts"
                defaultMessage="Keyboard shortcuts"
              />
            </caption>
            <thead>
              <tr>
                <th>
                  <FormattedMessage id="tv.text.key" defaultMessage="Key" />
                </th>
                <th>
                  <FormattedMessage
                    id="tv.text.action"
                    defaultMessage="Action"
                  />
                </th>
              </tr>
            </thead>
            <tbody>
              <tr>
                <td>↑ / ↓</td>
                <td>
                  <FormattedMessage
                    id="tv.text.previous_next_item"
                    defaultMessage="Previous / next item"
                  />
                </td>
              </tr>
              <tr>
                <td>← / →</td>
                <td>
                  <FormattedMessage
                    id="tv.text.marker_aware_seek_hold_for_reverse_faster_playback"
                    defaultMessage="Marker-aware seek; hold for reverse / faster playback"
                  />
                </td>
              </tr>
              <tr>
                <td>
                  <FormattedMessage
                    id="tv.text.during_a_hold"
                    defaultMessage="↑ / ↓ during a hold"
                  />
                </td>
                <td>
                  <FormattedMessage
                    id="tv.text.increase_decrease_held_speed"
                    defaultMessage="Increase / decrease held speed"
                  />
                </td>
              </tr>
              <tr>
                <td>
                  <FormattedMessage
                    id="tv.text.space_m"
                    defaultMessage="Space / M"
                  />
                </td>
                <td>
                  <FormattedMessage
                    id="tv.text.play_or_pause_mute"
                    defaultMessage="Play or pause / mute"
                  />
                </td>
              </tr>
              <tr>
                <td>
                  <FormattedMessage
                    id="tv.text.d_e_i"
                    defaultMessage="D / E / I"
                  />
                </td>
                <td>
                  <FormattedMessage
                    id="tv.text.delete_confirmation_edit_tags_information"
                    defaultMessage="Delete confirmation / edit tags / information"
                  />
                </td>
              </tr>
              <tr>
                <td>
                  <FormattedMessage id="tv.text.f_o" defaultMessage="F / O" />
                </td>
                <td>
                  <FormattedMessage
                    id="tv.text.fullscreen_or_rotate_the_presentation"
                    defaultMessage="Fullscreen when supported / rotate the presentation"
                  />
                </td>
              </tr>
              <tr>
                <td>
                  <FormattedMessage
                    id="tv.text.l_s_h"
                    defaultMessage="L / S / H"
                  />
                </td>
                <td>
                  <FormattedMessage
                    id="tv.text.playback_mode_subtitles_show_or_hide_controls"
                    defaultMessage="Playback mode / subtitles / show or hide controls"
                  />
                </td>
              </tr>
              <tr>
                <td>
                  <FormattedMessage
                    id="tv.text.escape"
                    defaultMessage="Escape"
                  />
                </td>
                <td>
                  <FormattedMessage
                    id="tv.text.close_a_menu_or_exit_fullscreen"
                    defaultMessage="Close a menu or exit fullscreen"
                  />
                </td>
              </tr>
            </tbody>
          </table>
          <p>
            <FormattedMessage
              id="tv.text.tv_settings_live_in_the_app_s_settings_tv_page"
              defaultMessage="TV settings live in the app’s Settings → TV page. One default quality applies to scenes and markers. A fixed quality chooses an advertised transcode at that resolution or below; if none is available, choose a temporary source explicitly. Source choices in TV apply to the current item."
            />
          </p>
          <p>
            <FormattedMessage
              id="tv.text.markers_play_the_parent_scene_from_their_start_to_a"
              defaultMessage="Markers play the parent scene from their start to a valid explicit end, the next later marker, or scene end. TV does not require generated video previews."
            />
          </p>
          <p>
            <FormattedMessage
              id="tv.text.fullscreen_when_supported"
              defaultMessage="Fullscreen is available only when the browser supports the entire TV interface in fullscreen. The video stays inline so navigation, menus and the rail remain available."
            />
          </p>
          <p>
            <FormattedMessage
              id="tv.text.activity_follows_the_main_app_s_tracking_settings_scene_views"
              defaultMessage="Activity follows the main app’s tracking settings. Scene views record actual watched time and resume position; marker clips and offline playback do not update server activity. Closing the browser may interrupt a final save. Uncertain writes are not retried automatically."
            />
          </p>
          <p>
            <FormattedMessage
              id="tv.text.the_feed_reflects_a_live_library_edits_can_change_upcoming"
              defaultMessage="The feed reflects a live library. Edits can change upcoming results; shuffle is stable for an unchanged result set. Returning from Settings or details restores this visit while it remains cached."
            />
          </p>
        </div>
      </TvDialogContent>
    </Dialog>
  );
}
