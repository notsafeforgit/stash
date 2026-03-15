import React, { useMemo, useState } from "react";
import {
  Button,
  Form,
  Spinner,
  Table,
  Row,
  Col,
  Card,
  ButtonGroup,
  OverlayTrigger,
  Tooltip,
} from "react-bootstrap";
import { FormattedMessage, useIntl } from "react-intl";
import { useFindDuplicateImagesQuery } from "src/core/generated-graphql";
import * as GQL from "src/core/generated-graphql";
import { PatchContainerComponent } from "src/patch";
import { LoadingIndicator } from "../Shared/LoadingIndicator";
import { ErrorMessage } from "../Shared/ErrorMessage";
import { FileSize } from "../Shared/FileSize";
import { Pagination } from "src/components/List/Pagination";
import { useHistory } from "react-router-dom";
import { DeleteImagesDialog } from "../Images/DeleteImagesDialog";
import { EditImagesDialog } from "../Images/EditImagesDialog";
import { Icon } from "../Shared/Icon";
import { faPencilAlt, faTrash } from "@fortawesome/free-solid-svg-icons";

const ImageDuplicateCheckerSection = PatchContainerComponent(
  "ImageDuplicateCheckerSection"
);

const ImageDuplicateChecker: React.FC = () => {
  const intl = useIntl();
  const history = useHistory();
  const query = new URLSearchParams(history.location.search);
  const currentPage = Number.parseInt(query.get("page") ?? "1", 10);
  const pageSize = Number.parseInt(query.get("size") ?? "20", 10);
  const hashDistance = Number.parseInt(query.get("distance") ?? "0", 10);

  const [isSearching, setIsSearching] = useState(false);
  const [hasSearched, setHasSearched] = useState(false);
  const [checkedImages, setCheckedImages] = useState<Record<string, boolean>>(
    {}
  );
  const [selectedImages, setSelectedImages] =
    useState<GQL.ImageDataFragment[]>();
  const [deletingImages, setDeletingImages] = useState(false);
  const [editingImages, setEditingImages] = useState(false);


  const { data: missingPhash } = GQL.useFindImagesQuery({
    variables: {
      filter: {
        per_page: 0,
      },
      image_filter: {
        is_missing: "phash",
      },
    },
  });

  function maybeRenderMissingPhashWarning() {
    const missingPhashes = missingPhash?.findImages.count ?? 0;
    if (missingPhashes > 0) {
      return (
        <p className="lead">
          <Icon icon={faExclamationTriangle} className="text-warning" />
          Missing phashes for {missingPhashes} images. Please run the phash
          generation task.
        </p>
      );
    }
  }

  const { data, loading, error, refetch } = useFindDuplicateImagesQuery({
    variables: { distance: hashDistance },
    skip: !hasSearched,
    fetchPolicy: "network-only",
  });

  const handleSearch = () => {
    setIsSearching(true);
    setHasSearched(true);
    setCheckedImages({});
    refetch({ distance: hashDistance }).finally(() => setIsSearching(false));
  };

  const getGroupTotalSize = (group: GQL.ImageDataFragment[]) => {
    return group.reduce((groupTotal, img) => {
      const imgTotal = img.visual_files.reduce(
        (fileTotal, file) => fileTotal + (file.size ?? 0),
        0
      );
      return groupTotal + imgTotal;
    }, 0);
  };

  const allGroups = useMemo(() => {
    const groups = data?.findDuplicateImages ?? [];
    return [...groups].sort((a, b) => {
      return getGroupTotalSize(b) - getGroupTotalSize(a);
    });
  }, [data?.findDuplicateImages]);

  const pagedGroups = useMemo(() => {
    const start = (currentPage - 1) * pageSize;
    return allGroups.slice(start, start + pageSize);
  }, [allGroups, currentPage, pageSize]);

  const checkCount = Object.keys(checkedImages).filter(
    (id) => checkedImages[id]
  ).length;

  const handleCheck = (checked: boolean, imageID: string) => {
    setCheckedImages({ ...checkedImages, [imageID]: checked });
  };

  const handleDeleteChecked = () => {
    setSelectedImages(allGroups.flat().filter((i) => checkedImages[i.id]));
    setDeletingImages(true);
  };

  const onEdit = () => {
    setSelectedImages(allGroups.flat().filter((i) => checkedImages[i.id]));
    setEditingImages(true);
    setCheckedImages({});
  };

  const onDeleteDialogClosed = (confirmed: boolean) => {
    setDeletingImages(false);
    setSelectedImages(undefined);
    if (confirmed) {
      setCheckedImages({});
      refetch();
    }
  };

  const onEditDialogClosed = (applied: boolean) => {
    setEditingImages(false);
    setSelectedImages(undefined);
    if (applied) {
      refetch();
    }
  };


  function renderPagination() {
    return (
      <div className="d-flex mt-2 mb-2">
        <h6 className="mr-auto align-self-center">
          <FormattedMessage
            id="dupe_check.found_sets"
            values={{ setCount: allGroups.length }}
          />
        </h6>
        {checkCount > 0 && (
          <ButtonGroup>
            <OverlayTrigger
              overlay={
                <Tooltip id="edit">
                  {intl.formatMessage({ id: "actions.edit" })}
                </Tooltip>
              }
            >
              <Button variant="secondary" onClick={onEdit}>
                <Icon icon={faPencilAlt} />
              </Button>
            </OverlayTrigger>
            <OverlayTrigger
              overlay={
                <Tooltip id="delete">
                  {intl.formatMessage({ id: "actions.delete" })}
                </Tooltip>
              }
            >
              <Button variant="danger" onClick={handleDeleteChecked}>
                <Icon icon={faTrash} />
              </Button>
            </OverlayTrigger>
          </ButtonGroup>
        )}
        <Pagination
          itemsPerPage={pageSize}
          currentPage={currentPage}
          totalItems={allGroups.length}
          metadataByline={[]}
          onChangePage={(newPage) => {
            setQuery({ page: newPage === 1 ? undefined : newPage });
            resetCheckboxSelection();
          }}
        />
        <Form.Control
          as="select"
          className="w-auto ml-2 btn-secondary"
          defaultValue={pageSize}
          value={currentPageSize}
          onChange={(e) => {
            setCurrentPageSize(parseInt(e.currentTarget.value, 10));
            setQuery({
              size:
                e.currentTarget.value === "20"
                  ? undefined
                  : e.currentTarget.value,
            });
            resetCheckboxSelection();
          }}
        >
          {pageOptions}
        </Form.Control>
      </div>
    );
  }

  function maybeRenderPopoverButtonGroup(image: GQL.ImageDataFragment) {
    if (
      image.tags.length > 0 ||
      image.performers.length > 0 ||
      image.galleries.length > 0 ||
      image.visual_files.length > 1 ||
      image.organized
    ) {
      return (
        <ButtonGroup className="flex-wrap">
          {image.tags.length > 0 && (
            <HoverPopover placement="bottom" content={image.tags.map((tag) => <TagLink key={tag.id} tag={tag} />)}>
              <Button className="minimal"><Icon icon={faTag} /><span>{image.tags.length}</span></Button>
            </HoverPopover>
          )}
          {image.performers.length > 0 && <PerformerPopoverButton performers={image.performers} />}
          {image.galleries.length > 0 && (
            <HoverPopover placement="bottom" content={image.galleries.map((g) => <GalleryLink key={g.id} gallery={g} />)}>
              <Button className="minimal"><Icon icon={faImages} /><span>{image.galleries.length}</span></Button>
            </HoverPopover>
          )}
          {image.visual_files.length > 1 && (
            <HoverPopover placement="bottom" content={<FormattedMessage id="files_amount" values={{ value: intl.formatNumber(image.visual_files.length) }} />}>
              <Button className="minimal"><Icon icon={faFileAlt} /><span>{image.visual_files.length}</span></Button>
            </HoverPopover>
          )}
          {image.organized && (
            <div><Button className="minimal"><Icon icon={faBox} /></Button></div>
          )}
        </ButtonGroup>
      );
    }
  }

  return (
    <Card id="image-duplicate-checker" className="col col-xl-12 mx-auto">
      <div className={CLASSNAME}>
        <ImageDuplicateCheckerSection>
          {deletingImages && selectedImages && (
            <DeleteImagesDialog
              selected={selectedImages}
              onClose={onDeleteDialogClosed}
            />
          )}
          {editingImages && selectedImages && (
            <EditImagesDialog
              selected={selectedImages}
              onClose={onEditDialogClosed}
            />
          )}

          <h4>
            <FormattedMessage id="config.tools.image_duplicate_checker" />
          </h4>

          <Form>
            <Form.Group>
              <Row noGutters>
                <Form.Label>
                  <FormattedMessage id="dupe_check.search_accuracy_label" />
                </Form.Label>
                <Col xs="auto">
                  <Form.Control
                    as="select"
                    onChange={(e) =>
                      setQuery({
                        distance:
                          e.currentTarget.value === "0"
                            ? undefined
                            : e.currentTarget.value,
                        page: undefined,
                      })
                    }
                    defaultValue={hashDistance}
                    className="input-control ml-4"
                  >
                    <option value={0}>
                      {intl.formatMessage({ id: "dupe_check.options.exact" })}
                    </option>
                    <option value={4}>
                      {intl.formatMessage({ id: "dupe_check.options.high" })}
                    </option>
                    <option value={8}>
                      {intl.formatMessage({ id: "dupe_check.options.medium" })}
                    </option>
                    <option value={10}>
                      {intl.formatMessage({ id: "dupe_check.options.low" })}
                    </option>
                  </Form.Control>
                </Col>
              </Row>
              <Form.Text>
                <FormattedMessage id="dupe_check.description" />
              </Form.Text>
            </Form.Group>

            <Form.Group>
              <Row noGutters>
                <Col xs="12">
                  <Dropdown className="">
                    <Dropdown.Toggle variant="secondary">
                      <FormattedMessage id="dupe_check.select_options" />
                    </Dropdown.Toggle>
                    <Dropdown.Menu className="bg-secondary text-white">
                      <Dropdown.Item onClick={() => resetCheckboxSelection()}>
                        {intl.formatMessage({ id: "dupe_check.select_none" })}
                      </Dropdown.Item>

                      <Dropdown.Item
                        onClick={() => onSelectLargestResolutionClick()}
                      >
                        {intl.formatMessage({
                          id: "dupe_check.select_all_but_largest_resolution",
                        })}
                      </Dropdown.Item>

                      <Dropdown.Item onClick={() => onSelectLargestClick()}>
                        {intl.formatMessage({
                          id: "dupe_check.select_all_but_largest_file",
                        })}
                      </Dropdown.Item>

                      <Dropdown.Item onClick={() => onSelectByAge(true)}>
                        {intl.formatMessage({
                          id: "dupe_check.select_oldest",
                        })}
                      </Dropdown.Item>

                      <Dropdown.Item onClick={() => onSelectByAge(false)}>
                        {intl.formatMessage({
                          id: "dupe_check.select_youngest",
                        })}
                      </Dropdown.Item>
                    </Dropdown.Menu>
                  </Dropdown>
                </Col>
              </Row>
            </Form.Group>
          </Form>

          {maybeRenderMissingPhashWarning()}
          {renderPagination()}

          <Table responsive striped className={`${CLASSNAME}-table`}>
            <colgroup>
              <col className={`${CLASSNAME}-checkbox`} />
              <col className={`${CLASSNAME}-sprite`} />
              <col className={`${CLASSNAME}-title`} />
              <col className={`${CLASSNAME}-details`} />
              <col className={`${CLASSNAME}-filesize`} />
              <col className={`${CLASSNAME}-resolution`} />
              <col className={`${CLASSNAME}-operations`} />
            </colgroup>
            <thead>
              <tr>
                <th> </th>
                <th> </th>
                <th>{intl.formatMessage({ id: "details" })}</th>
                <th> </th>
                <th>{intl.formatMessage({ id: "filesize" })}</th>
                <th>{intl.formatMessage({ id: "resolution" })}</th>
                <th>{intl.formatMessage({ id: "actions.delete" })}</th>
              </tr>
            </thead>
            <tbody>
              {pagedGroups.map((group, groupIndex) =>
                group.map((image, i) => {
                  const file = image.visual_files[0];

                  return (
                    <React.Fragment key={image.id}>
                      {i === 0 && groupIndex !== 0 ? (
                        <tr className="separator" />
                      ) : undefined}
                      <tr
                        className={i === 0 ? "duplicate-group" : ""}
                        key={image.id}
                      >
                        <td>
                          <Form.Check
                            checked={checkedImages[image.id] || false}
                            onChange={(e) =>
                              handleCheck(e.currentTarget.checked, image.id)
                            }
                          />
                        </td>
                        <td>
                          <HoverPopover
                            content={
                              <img
                                src={image.paths.thumbnail || ""}
                                alt=""
                                style={{ maxWidth: 600, maxHeight: 600, objectFit: "contain" }}
                              />
                            }
                            placement="right"
                          >
                            <img
                              src={image.paths.thumbnail || ""}
                              alt=""
                              style={{
                                maxWidth: "120px",
                                maxHeight: "120px",
                                objectFit: "contain",
                                border: checkedImages[image.id] ? "2px solid red" : "",
                              }}
                            />
                          </HoverPopover>
                        </td>
                        <td className="text-left">
                          <p>
                            <Link
                              to={`/images/${image.id}`}
                              style={{
                                fontWeight: checkedImages[image.id] ? "bold" : "inherit",
                                textDecoration: checkedImages[image.id] ? "line-through 3px" : "inherit",
                                textDecorationColor: checkedImages[image.id] ? "red" : "inherit",
                              }}
                            >
                              {image.title || TextUtils.fileNameFromPath(file?.path ?? "")}
                            </Link>
                          </p>
                          <p className="scene-path">{file?.path ?? ""}</p>
                        </td>
                        <td className="scene-details">
                          {maybeRenderPopoverButtonGroup(image)}
                        </td>
                        <td>
                          <FileSize size={file?.size ?? 0} />
                        </td>
                        <td>
                          {file?.__typename === "ImageFile" || file?.__typename === "VideoFile" ? (
                            <>{file.width ?? 0}x{file.height ?? 0}</>
                          ) : (
                            "N/A"
                          )}
                        </td>
                        <td>
                          <Button
                            className="edit-button"
                            variant="danger"
                            onClick={() => handleDeleteImage(image)}
                          >
                            <FormattedMessage id="actions.delete" />
                          </Button>
                        </td>
                      </tr>
                    </React.Fragment>
                  );
                })
              )}
            </tbody>
          </Table>

          {allGroups.length === 0 && (
            <h4 className="text-center mt-4">No duplicates found.</h4>
          )}

          {renderPagination()}
        </ImageDuplicateCheckerSection>
      </div>
    </Card>
  );
};

export default ImageDuplicateChecker;
